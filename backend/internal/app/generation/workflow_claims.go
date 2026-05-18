package generation

import (
	"context"
	"fmt"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// stageClaim is the outcome of acquireStageClaim. When replayed is true the
// caller skips the side effect and uses replayResult (an asset id or staging
// key, depending on stage). Otherwise token is the lease handle to pass to
// Complete/Fail/Abandon.
type stageClaim struct {
	token        string
	replayed     bool
	replayResult string
}

// acquireStageClaim brackets a stage with the shared claim policy and maps
// the classified result into stage-level error semantics. stageCode prefixes
// error codes (e.g. "PROVIDER_SUBMIT", "DISCLOSURE_POSTPROCESS"). With no
// Idempotency store wired (test path) returns a zero claim and nil so callers
// proceed unguarded.
func (w *Workflow) acquireStageClaim(ctx context.Context, scope, inputHash, stageCode string) (stageClaim, error) {
	if w.Idempotency == nil {
		return stageClaim{}, nil
	}
	acquired, err := idempotency.Acquire(ctx, w.Idempotency, scope, inputHash, w.StageLease)
	if err != nil {
		return stageClaim{}, fmt.Errorf("%s: %w", strings.ToLower(stageCode), err)
	}
	switch acquired.Kind {
	case idempotency.AcquireOwned, idempotency.AcquireReclaimed:
		return stageClaim{token: acquired.Token}, nil
	case idempotency.AcquireCompleted:
		return stageClaim{replayed: true, replayResult: acquired.CachedRef}, nil
	case idempotency.AcquireInFlight:
		return stageClaim{}, generation.Transient(stageCode+"_IN_FLIGHT", "another worker holds the "+strings.ToLower(stageCode)+" claim")
	case idempotency.AcquirePermanentlyFailed:
		return stageClaim{}, generation.Terminal(stageCode+"_PERMANENT_FAILURE", "claim already failed")
	case idempotency.AcquireInputConflict:
		return stageClaim{}, generation.Terminal(stageCode+"_INPUT_CONFLICT", "input hash conflict for stable claim")
	}
	return stageClaim{}, fmt.Errorf("%s: unknown acquire kind %d", strings.ToLower(stageCode), acquired.Kind)
}

// claimAbandon releases an in-flight stage claim. No-op when there's no
// idempotency store (test path) or no token (replayed cache hit). Errors are
// swallowed: the lease will time out naturally if the abandon write fails.
func (w *Workflow) claimAbandon(ctx context.Context, scope, token string) {
	if w.Idempotency == nil || token == "" {
		return
	}
	_ = w.Idempotency.Abandon(ctx, scope, token)
}

// claimFailOrAbandon dispatches on err's terminal flag: terminal errors
// permanently fail the claim so retries short-circuit; transient errors
// abandon so the next retry can re-acquire.
func (w *Workflow) claimFailOrAbandon(ctx context.Context, scope, token string, err error) {
	if w.Idempotency == nil || token == "" {
		return
	}
	if generation.IsTerminal(err) {
		_ = w.Idempotency.Fail(ctx, scope, token, generation.AsError(err).Code)
		return
	}
	_ = w.Idempotency.Abandon(ctx, scope, token)
}

// claimFail permanently fails the claim with an explicit error code. Used
// when the caller already classified the failure (e.g. gate decision).
func (w *Workflow) claimFail(ctx context.Context, scope, token, code string) {
	if w.Idempotency == nil || token == "" {
		return
	}
	_ = w.Idempotency.Fail(ctx, scope, token, code)
}

// claimComplete records the claim's cached result. Returns nil when there's
// no store or no token; otherwise propagates the store error so the caller
// can roll back side effects on a failed complete.
func (w *Workflow) claimComplete(ctx context.Context, scope, token, result string) error {
	if w.Idempotency == nil || token == "" {
		return nil
	}
	return w.Idempotency.Complete(ctx, scope, token, result)
}

// genScope builds the idempotency scope string for a generation stage. The
// "GEN#<jobID>" prefix matches the DDB job sort key (ddb.GenSK) so claims for
// the same job cluster on the same partition.
func genScope(jobID, stage string) string {
	return "GEN#" + jobID + "#" + stage
}

func hashJobInput(job *generation.Job) string {
	return idempotency.HashInputs(job.TenantID, job.ID, job.Prompt, job.Provider, job.Model, string(job.OutputType))
}

func hashProviderInput(job *generation.Job, provider string) string {
	return idempotency.HashInputs(
		job.TenantID,
		job.ID,
		string(job.OutputType),
		provider,
		job.Model,
		job.PreparedPromptHash,
		job.GenerationParamsHash,
		"provider-idempotency-v1",
	)
}
