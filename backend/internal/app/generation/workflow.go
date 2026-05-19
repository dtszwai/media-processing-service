package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// Workflow executes the generation FSM stage by stage. Each stage produces a
// StageResult; AdvanceStageAndEnqueue applies the result atomically together
// with the outbox row for the next stage's SNS publish.
//
// In production the loop is driven by SQS stage messages: each worker
// invocation calls RunStage for exactly one stage and exits. Run() preserves
// the synchronous end-to-end driver used by tests and the in-process poller.
//
// Stage responsibilities are split sharply: INFER calls the provider and
// drops the raw artifact into Stager (short-TTL staging area). DISCLOSURE_POSTPROCESS
// is the *only* place that runs the AI-disclosure gate and writes the final
// asset. This split exists so byte-level mutations (watermark, EXIF strip,
// ICC normalize) can land between provider output and final sink without
// races against the inference claim — the gate runs against the *mutated*
// bytes so disclosures computed at postprocess time (watermark fingerprint,
// stamped hash) are gate-visible.
type Workflow struct {
	Repo             JobRepository
	Provider         genprovider.Provider
	Idempotency      idempotency.Store
	ArtifactSink     ArtifactSink
	Stager           StagedArtifactStore
	QuotaReserver    QuotaReserver
	QuotaLedger      QuotaLedger
	ProviderRequests ProviderRequestRepository
	PromptSealer     PromptSealer
	PromptEnhancer   PromptEnhancer
	MaxRetries       int
	Clock            func() time.Time
	StageLease       time.Duration
	// StagedTTL is how long the staging area honours a freshly staged
	// artifact before DISCLOSURE_POSTPROCESS must reject it.
	StagedTTL time.Duration
	// ImageStamper applies the visible watermark to image artifacts in
	// DISCLOSURE_POSTPROCESS. AGENTS.md "AI-disclosure gate per output type" makes
	// this load-bearing for image outputs — production must wire a real
	// stamper or the gate will reject every image artifact (its
	// `watermark.fingerprint` field will be empty).
	ImageStamper *postprocess.Stamper
	// LeaseRunner gates provider calls on a DDB-backed LEASE# row so two
	// workers never call the provider concurrently for the same resource
	// class. Nil disables leasing (test path).
	LeaseRunner *LeaseScopedRunner
	// LeaseTTL is the deadline a freshly acquired lease honours before the
	// reaper considers it abandoned. Should comfortably exceed the
	// provider's worst-case latency for the stage.
	LeaseTTL time.Duration
	// Moderator is the safety port the INPUT_MODERATION and
	// OUTPUT_MODERATION stages call. Nil short-circuits to PASS so test
	// fixtures don't have to wire a classifier; production must inject a
	// real one (SimulatedModerator for local dev, a provider-backed impl
	// in production) or the safety gates are no-ops.
	Moderator safetyapp.Moderator
	// AuditRecorder writes the safety.input_moderation.decided and
	// safety.output_moderation.decided audit rows. Nil is permissive (the
	// stage still runs) so the test path doesn't need to wire an audit
	// store; production wires the standalone DDB-backed Recorder.
	AuditRecorder auditapp.Recorder
	UsageMeter    UsageMeter
	// Instruments is the OTEL counter / histogram bundle the FSM emits to.
	// Nil is replaced by obs.Noop() in NewWorkflow so production code paths
	// never have to nil-check before calling Add/Record.
	Instruments *obs.Instruments
}

const (
	ServiceCostSourceProviderSubmit = "provider-submit"
	ServiceCostSourcePromptEnhance  = "prompt-enhance"
)

// QuotaReserver gates the COST_RESERVE stage and exposes commit/release
// for the terminal transition. Reserve must NOT contact the provider — it is
// the gate that decides whether the provider is allowed to be called.
// Ensure bootstraps the daily aggregate Reservoir row; required by
// callers that run the aggregate decrement inside a TransactWrite (which
// can't create the row on demand). Idempotent. The interface stays scope-
// agnostic so an API-key-cost / vendor-cost reservoir can plug in here.
type QuotaReserver interface {
	Ensure(ctx context.Context, tenantID, period string) error
	Reserve(ctx context.Context, tenantID, period string, amount int64) (granted bool, remaining int64, err error)
	Commit(ctx context.Context, tenantID, period string, amount int64) error
	Release(ctx context.Context, tenantID, period string, amount int64) error
}

// PromptSealer seals/unseals prompts via KMS envelope. The repo boundary calls
// Seal on write and Unseal on read so the domain Job remains plaintext while
// at-rest storage is encrypted.
type PromptSealer interface {
	Seal(ctx context.Context, tenantID, jobID, plaintext string) ([]byte, error)
	Unseal(ctx context.Context, tenantID, jobID string, ciphertext []byte) (string, error)
}

// NewWorkflow validates a partially-filled Workflow, applies defaults, and
// returns the runtime instance. Repo and Provider are mandatory; everything
// else may be left zero for tests. Callers serving real traffic must
// additionally call ValidateProduction(outputType) so missing production-only
// deps (e.g. LeaseRunner, Stager, ImageStamper for images) surface at first
// dispatch rather than gate-rejection time. Stager and ImageStamper are
// intentionally not defaulted so ValidateProduction's checks stay load-bearing.
func NewWorkflow(w Workflow) (*Workflow, error) {
	if w.Repo == nil {
		return nil, errors.New("generation: NewWorkflow requires Repo")
	}
	if w.Provider == nil {
		return nil, errors.New("generation: NewWorkflow requires Provider")
	}
	if w.MaxRetries == 0 {
		w.MaxRetries = 3
	}
	if w.Clock == nil {
		w.Clock = func() time.Time { return time.Now().UTC() }
	}
	if w.StageLease == 0 {
		w.StageLease = 5 * time.Minute
	}
	if w.StagedTTL == 0 {
		w.StagedTTL = 24 * time.Hour
	}
	if w.Instruments == nil {
		// A nil instrument bundle is the test path; wire a no-op so the
		// dispatch wrapper does not have to branch on Instruments != nil at
		// every Add/Record site.
		w.Instruments = obs.Noop()
	}
	if w.ProviderRequests == nil {
		if repo, ok := w.Repo.(ProviderRequestRepository); ok {
			w.ProviderRequests = repo
		}
	}
	return &w, nil
}

// ValidateProduction returns an error naming dependencies a production
// Workflow must have wired but the receiver is missing. Image workflows must
// additionally have an ImageStamper, because the AI-disclosure gate in
// DISCLOSURE_POSTPROCESS rejects image artifacts whose visible-watermark fingerprint is
// empty. Callers serving real traffic should call this once per OutputType.
func (w *Workflow) ValidateProduction(outputType generation.OutputType) error {
	var missing []string
	if w.Idempotency == nil {
		missing = append(missing, "Idempotency")
	}
	if w.ArtifactSink == nil {
		missing = append(missing, "ArtifactSink")
	}
	if w.Stager == nil {
		missing = append(missing, "Stager")
	}
	if w.QuotaReserver == nil {
		missing = append(missing, "QuotaReserver")
	}
	if w.QuotaLedger == nil {
		missing = append(missing, "QuotaLedger")
	}
	if w.PromptSealer == nil {
		missing = append(missing, "PromptSealer")
	}
	if w.ProviderRequests == nil {
		missing = append(missing, "ProviderRequests")
	}
	if w.LeaseRunner == nil {
		missing = append(missing, "LeaseRunner")
	}
	if w.Moderator == nil {
		missing = append(missing, "Moderator")
	}
	if w.PromptEnhancer == nil {
		missing = append(missing, "PromptEnhancer")
	}
	if w.AuditRecorder == nil {
		missing = append(missing, "AuditRecorder")
	}
	if outputType == generation.OutputImage && w.ImageStamper == nil {
		missing = append(missing, "ImageStamper")
	}
	if len(missing) > 0 {
		return fmt.Errorf("generation: production workflow missing deps: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (w *Workflow) now() time.Time {
	return w.Clock()
}
