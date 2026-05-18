package generation

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

// RunStage executes the side effects for the current stage and returns the
// StageResult to be persisted by AdvanceStageAndEnqueue. Worker entry point.
// Per-stage handlers live in workflow_<stage>.go.
//
// The dispatch is wrapped with the OTEL instrument set: every call increments
// workflow.stage_started_total on entry, records workflow.stage_latency_ms on
// exit, and increments workflow.stage_completed_total with the classified
// result. The terminal counter (workflow.terminal_total) is emitted by Run
// and AdvanceOneStage rather than here, because RunStage doesn't always lead
// to a terminal transition — only the driver knows when the FSM has landed.
func (w *Workflow) RunStage(ctx context.Context, job *generation.Job) (StageResult, error) {
	stage := string(job.CurrentStage)
	work := string(stageWorkClass(job))
	tier := string(job.Tier)
	output := string(job.OutputType)

	startedAttrs := metric.WithAttributes(
		attribute.String("stage", stage),
		attribute.String("work_class", work),
		attribute.String("tier", tier),
		attribute.String("output_type", output),
	)
	w.Instruments.WorkflowStageStarted.Add(ctx, 1, startedAttrs)

	start := w.now()
	result, runErr := w.runStageInner(ctx, job)
	elapsedMs := float64(w.now().Sub(start)) / float64(time.Millisecond)

	// Provider attribute on latency is best-effort — most stages don't talk
	// to a provider so the label collapses to "n/a". The dashboard query
	// filters by stage so the per-stage provider mix stays readable.
	provider := "n/a"
	if w.Provider != nil && (job.CurrentStage == generation.StageProviderSubmit || job.CurrentStage == generation.StageProviderWait) {
		provider = providerName(w.Provider)
	}
	w.Instruments.WorkflowStageLatency.Record(ctx, elapsedMs, metric.WithAttributes(
		attribute.String("stage", stage),
		attribute.String("work_class", work),
		attribute.String("provider", provider),
		attribute.String("output_type", output),
	))

	resultLabel, errorCode := classifyStageOutcome(runErr)
	w.Instruments.WorkflowStageCompleted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("stage", stage),
		attribute.String("result", resultLabel),
		attribute.String("error_code", errorCode),
	))
	return result, runErr
}

// runStageInner is the original dispatch — kept separate from RunStage so the
// instrument wrapper is one well-defined critical region and each per-stage
// handler stays free of telemetry concerns.
func (w *Workflow) runStageInner(ctx context.Context, job *generation.Job) (StageResult, error) {
	switch job.CurrentStage {
	case generation.StageInputModeration:
		return w.stageInputModeration(ctx, job)
	case generation.StageCostReserve:
		return w.stageQuotaReserve(ctx, job)
	case generation.StagePromptPrepare:
		return w.stagePreprocess(ctx, job)
	case generation.StageProviderSubmit:
		return w.stageInference(ctx, job)
	case generation.StageProviderWait:
		return w.stageInferencePoll(ctx, job)
	case generation.StageOutputModeration:
		return w.stageOutputModeration(ctx, job)
	case generation.StageDisclosurePostprocess:
		return w.stagePostprocess(ctx, job)
	case generation.StagePublish:
		return w.stageDelivery(ctx, job)
	default:
		return StageResult{}, generation.Terminal("UNKNOWN_STAGE", string(job.CurrentStage))
	}
}

// classifyStageOutcome maps a stage handler's error into the enum-shaped
// (result, error_code) attribute pair the dashboards filter on. Free-form
// error messages never reach a metric attribute — only the generation.Error
// classification (Code + Terminal) does.
func classifyStageOutcome(err error) (string, string) {
	if err == nil {
		return obs.ResultSuccess, "none"
	}
	classified := generation.AsError(err)
	code := classified.Code
	if code == "" {
		code = "UNKNOWN_ERROR"
	}
	if classified.Terminal {
		return obs.ResultTerminalFail, code
	}
	return obs.ResultTransientFail, code
}

// stageWorkClass maps the persisted CurrentStage to the work_class label
// the dashboards filter on. It mirrors transition routing so metric labels
// and queue classes stay in the same closed set.
func stageWorkClass(job *generation.Job) generation.ResourceClass {
	switch job.CurrentStage {
	case generation.StageInputModeration, generation.StageOutputModeration,
		generation.StageCostReserve, generation.StagePromptPrepare, generation.StagePublish:
		return generation.ResourceFast
	case generation.StageProviderSubmit:
		return generation.ResourceProvider
	case generation.StageProviderWait:
		return generation.ResourcePoll
	case generation.StageDisclosurePostprocess:
		return resourceClassForPostprocess(job)
	default:
		return generation.ResourceFast
	}
}

// emitTerminal increments workflow.terminal_total when a job lands in a
// terminal status. Called by Run + AdvanceOneStage after the persisted
// transition; idempotent against double-call because both call sites guard
// against re-emit by checking that the persisted transition just happened.
func (w *Workflow) emitTerminal(ctx context.Context, status generation.Status, errCode, outputType string) {
	if errCode == "" {
		errCode = "none"
	}
	w.Instruments.WorkflowTerminal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", string(status)),
		attribute.String("error_code", errCode),
		attribute.String("output_type", outputType),
	))
}

func (w *Workflow) emitCostReserve(ctx context.Context, outcome string, job *generation.Job) {
	if w.Instruments == nil {
		return
	}
	w.Instruments.CostReserve.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("output_type", string(job.OutputType)),
		attribute.String("tier", string(job.Tier)),
	))
}

// providerName extracts the vendor identifier for telemetry. Adapters
// implement genprovider.Named to advertise their canonical name; an
// adapter that omits the method shows up as "unknown" in dashboards rather
// than crashing the stage.
func providerName(p genprovider.Provider) string {
	if named, ok := p.(genprovider.Named); ok {
		return named.Name()
	}
	return "unknown"
}

// emitProviderCall records the provider.requests_total counter and the
// provider.request_latency_ms histogram for one adapter call. mode is one
// of obs.ProviderModeSync / ProviderModeSubmit / ProviderModePoll /
// ProviderModeFetch — matched to which method on the Provider port was
// invoked.
func (w *Workflow) emitProviderCall(ctx context.Context, provider, model, mode string, elapsedMs float64, err error) {
	result := obs.ResultSuccess
	if err != nil {
		if generation.IsTerminal(err) {
			result = obs.ResultTerminalFail
		} else {
			result = obs.ResultTransientFail
		}
	}
	if model == "" {
		model = "default"
	}
	w.Instruments.ProviderRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("result", result),
	))
	w.Instruments.ProviderRequestLatency.Record(ctx, elapsedMs, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("mode", mode),
	))
}
