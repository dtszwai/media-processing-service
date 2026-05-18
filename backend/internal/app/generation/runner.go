package generation

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// ProviderResolver picks the right adapter for a given output type. Satisfied
// by bootstrap.ProviderRegistry in production.
type ProviderResolver interface {
	PickForJob(generation.OutputType, string) (genprovider.Provider, error)
}

// StageRunner bundles the production stage-message processing deps so the
// API in-process poller and the standalone generation-worker Lambda share one
// path. Workflows are lazily constructed per OutputType and cached so per-
// message processing is allocation-free aside from the FSM step itself.
type StageRunner struct {
	Repo           JobRepository
	Idem           idempotency.Store
	Sink           ArtifactSink
	Stager         StagedArtifactStore
	ImageStamper   *postprocess.Stamper
	LeaseRunner    *LeaseScopedRunner
	Quota          QuotaReserver
	Ledger         QuotaLedger
	Sealer         PromptSealer
	Pickers        ProviderResolver
	Moderator      safetyapp.Moderator
	PromptEnhancer PromptEnhancer
	AuditRecorder  auditapp.Recorder
	UsageMeter     UsageMeter
	Instruments    *obs.Instruments

	workflows sync.Map // workflowCacheKey -> *Workflow
}

type workflowCacheKey struct {
	OutputType generation.OutputType
	Provider   string
}

var stageTracer = otel.Tracer("github.com/dtszwai/media-processing-service/backend/internal/app/generation")

// ProcessMessage runs one stage for the (tenant, job, stage) tuple described
// by body (already-unwrapped SNS payload). Idempotent: a stage message whose
// stage no longer matches the persisted CurrentStage is a no-op.
func (r *StageRunner) ProcessMessage(ctx context.Context, body []byte) (err error) {
	msg, err := UnmarshalStageMessage(body)
	if err != nil {
		return err
	}
	if msg.TenantID == "" || msg.JobID == "" || msg.Stage == "" || msg.StageVersion == 0 {
		return errors.New("stage message missing tenant_id/job_id/stage/stage_version")
	}
	ctx = ContextWithTraceparent(ctx, msg.Traceparent)
	ctx, span := stageTracer.Start(ctx, "generation.stage",
		trace.WithAttributes(
			attribute.String("tenant_id", msg.TenantID),
			attribute.String("job_id", msg.JobID),
			attribute.String("stage", string(msg.Stage)),
			attribute.Int64("stage_version", int64(msg.StageVersion)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	job, err := r.Repo.GetJob(ctx, msg.TenantID, msg.JobID)
	if err != nil {
		return err
	}
	if job.CurrentStage != msg.Stage {
		slog.InfoContext(ctx, "dropping stale generation stage", "job_id", msg.JobID, "message_stage", msg.Stage, "current_stage", job.CurrentStage)
		return nil
	}
	if job.StageVersion != msg.StageVersion {
		slog.InfoContext(ctx, "dropping stale generation stage version", "job_id", msg.JobID, "message_version", msg.StageVersion, "current_version", job.StageVersion)
		return nil
	}
	wf, err := r.workflowFor(job.OutputType, job.Provider)
	if err != nil {
		if generation.IsTerminal(err) {
			result := StageResult{
				NextStage:     StageTerminal,
				ResourceClass: stageWorkClass(job),
				TerminalError: generation.AsError(err),
			}
			if r.Ledger != nil && job.BudgetDate != "" && BudgetReleaseAllowed(job.CurrentStage) {
				op := r.Ledger.LedgerUpdateReleased(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
				result.LedgerOp = &op
			}
			return r.Repo.AdvanceStageAndEnqueue(ctx, job, result)
		}
		return err
	}
	return wf.AdvanceOneStage(ctx, job)
}

// workflowFor returns the cached *Workflow for outputType, constructing it on
// first use. The cache is provider-aware because a single output type may now
// run through multiple adapters in the same worker. The workflow is validated
// against the production-deps checklist before it is cached — a missing dep
// here is a wiring bug we want to surface at first dispatch, not at
// gate-rejection time.
func (r *StageRunner) workflowFor(outputType generation.OutputType, requestedProvider string) (*Workflow, error) {
	provider, err := r.Pickers.PickForJob(outputType, requestedProvider)
	if err != nil {
		return nil, err
	}
	key := workflowCacheKey{OutputType: outputType, Provider: providerName(provider)}
	if w, ok := r.workflows.Load(key); ok {
		return w.(*Workflow), nil
	}
	wf, err := NewWorkflow(Workflow{
		Repo:           r.Repo,
		Provider:       provider,
		Idempotency:    r.Idem,
		ArtifactSink:   r.Sink,
		Stager:         r.Stager,
		QuotaReserver:  r.Quota,
		QuotaLedger:    r.Ledger,
		PromptSealer:   r.Sealer,
		ImageStamper:   r.ImageStamper,
		LeaseRunner:    r.LeaseRunner,
		Moderator:      r.Moderator,
		PromptEnhancer: r.PromptEnhancer,
		AuditRecorder:  r.AuditRecorder,
		UsageMeter:     r.UsageMeter,
		Instruments:    r.Instruments,
	})
	if err != nil {
		return nil, err
	}
	if err := wf.ValidateProduction(outputType); err != nil {
		return nil, err
	}
	actual, _ := r.workflows.LoadOrStore(key, wf)
	return actual.(*Workflow), nil
}
