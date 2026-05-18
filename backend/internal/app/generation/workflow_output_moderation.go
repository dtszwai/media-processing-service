package generation

import (
	"context"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

// stageOutputModeration is the platform-attested safety gate between
// provider work and disclosure postprocess. It loads the staged artifact,
// runs the moderator against it, and advances to DISCLOSURE_POSTPROCESS only on PASS.
//
// Position is load-bearing: running BEFORE DISCLOSURE_POSTPROCESS means a FAIL terminates
// the job without ever mutating the customer-visible bytes (no watermark
// stamping, no final-sink write). The provider has already been charged for
// the work, so a fail does NOT release budget — the cost-commit happened
// upstream in PROVIDER_SUBMIT.
//
// Idempotent at the stage level via a stable claim. A replayed claim
// short-circuits the moderation call and re-emits the same transition
// (PASS advances to DISCLOSURE_POSTPROCESS, FAIL terminates).
func (w *Workflow) stageOutputModeration(ctx context.Context, job *generation.Job) (StageResult, error) {
	scope := genScope(job.ID, "OUTPUT_MODERATION")
	claim, err := w.acquireStageClaim(ctx, scope, hashJobInput(job), "OUTPUT_MODERATION")
	if err != nil {
		return StageResult{}, err
	}
	if claim.replayed {
		return w.nextStageResult(ctx, job, generation.StageDisclosurePostprocess, resourceClassForPostprocess(job)), nil
	}
	token := claim.token

	// Reuse the same staged-artifact loader that DISCLOSURE_POSTPROCESS uses. The
	// loader hides the difference between "staged bytes still warm" and
	// "lifecycle swept them after 24h" — the workflow surfaces the latter
	// as terminal STAGED_EXPIRED, same as DISCLOSURE_POSTPROCESS does, so the failure
	// mode is uniform across both downstream stages.
	_, art, lerr := w.loadStagedForPostprocess(ctx, *job)
	if lerr != nil {
		w.claimFailOrAbandon(ctx, scope, token, lerr)
		return StageResult{}, lerr
	}

	verdict, mErr := w.moderate(ctx, safetyapp.ModerateInput{
		Layer:      safety.LayerOutputModeration,
		TenantID:   job.TenantID,
		JobID:      job.ID,
		OutputType: job.OutputType,
		Artifact:   &art,
	})
	if mErr != nil {
		w.claimFailOrAbandon(ctx, scope, token, mErr)
		return StageResult{}, generation.Transient("OUTPUT_MODERATION_PROVIDER_ERROR", mErr.Error())
	}

	w.recordModerationEvent(ctx, auditapp.NewSafetyOutputModerationDecided(
		job.TenantID, job.ID,
		string(verdict.Decision), verdict.ReasonCode, verdict.PolicyVersion, verdict.Provider, verdict.Model,
		art.SHA256,
	), verdict.CreatedAt)

	w.emitSafetyDecision(ctx, verdict)

	switch verdict.Decision {
	case safety.DecisionPass:
		if cerr := w.claimComplete(ctx, scope, token, "PASS"); cerr != nil {
			return StageResult{}, cerr
		}
		return w.nextStageResult(ctx, job, generation.StageDisclosurePostprocess, resourceClassForPostprocess(job)), nil

	case safety.DecisionFail:
		w.claimFail(ctx, scope, token, "SAFETY_BLOCKED")
		return StageResult{}, generation.Terminal("SAFETY_BLOCKED", reasonOr(verdict.ReasonCode, "output moderation blocked the artifact"))

	case safety.DecisionReview:
		w.claimFail(ctx, scope, token, "SAFETY_BLOCKED_PENDING_REVIEW")
		return StageResult{}, generation.Terminal("SAFETY_BLOCKED", reasonOr(verdict.ReasonCode, "output moderation pending review"))
	}

	w.claimAbandon(ctx, scope, token)
	return StageResult{}, generation.Transient("OUTPUT_MODERATION_UNKNOWN_VERDICT", string(verdict.Decision))
}
