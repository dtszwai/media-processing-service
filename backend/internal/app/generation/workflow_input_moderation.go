package generation

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

// stageInputModeration is the accepted-job FSM's first stage. Running BEFORE
// BudgetReserve is load-bearing: a FAIL or REVIEW verdict here MUST NOT have
// reserved tenant budget, because the provider was never going to be called.
// A pre-submit capacity hint may reject unaffordable requests before job
// creation, but it remains read-only; COST_RESERVE is the authoritative budget
// reservation gate for accepted jobs.
//
// The handler is idempotent at the stage level via a stable claim keyed on
// the prompt+model input hash. A replayed claim simply re-emits the same
// transition (PASS advances, FAIL terminates) without calling the moderator
// twice. Audit rows are immutable so a replay never produces a duplicate row
// — the recorder's attribute_not_exists guard collapses re-records to nil.
func (w *Workflow) stageInputModeration(ctx context.Context, job *generation.Job) (StageResult, error) {
	scope := genScope(job.ID, "INPUT_MODERATION")
	claim, err := w.acquireStageClaim(ctx, scope, hashJobInput(job), "INPUT_MODERATION")
	if err != nil {
		return StageResult{}, err
	}
	if claim.replayed {
		// Replay of a PASS — the previous run already advanced through
		// BudgetReserve. Re-emitting BudgetReserve is correct: AdvanceStage
		// is conditional on CurrentStage = StageInputModeration, so the txn
		// is a no-op when the job has already moved on.
		return StageResult{Outcome: OutcomeModerationPassed}, nil
	}
	token := claim.token

	verdict, mErr := w.moderate(ctx, safetyapp.ModerateInput{
		Layer:      safety.LayerInputModeration,
		TenantID:   job.TenantID,
		JobID:      job.ID,
		OutputType: job.OutputType,
		Model:      job.Model,
		Prompt:     job.Prompt,
	})
	if mErr != nil {
		w.claimFailOrAbandon(ctx, scope, token, mErr)
		// Surfaced as transient so SQS retries pick it up after backoff.
		return StageResult{}, generation.Transient("INPUT_MODERATION_PROVIDER_ERROR", mErr.Error())
	}

	w.recordModerationEvent(ctx, auditapp.NewSafetyInputModerationDecided(
		job.TenantID, job.ID,
		string(verdict.Decision), verdict.ReasonCode, verdict.PolicyVersion, verdict.Provider, verdict.Model,
	), verdict.CreatedAt)

	w.emitSafetyDecision(ctx, verdict)

	switch verdict.Decision {
	case safety.DecisionPass:
		if cerr := w.claimComplete(ctx, scope, token, "PASS"); cerr != nil {
			return StageResult{}, cerr
		}
		return StageResult{Outcome: OutcomeModerationPassed}, nil

	case safety.DecisionFail:
		// SAFETY_BLOCKED is terminal — surface through the workflow's
		// classified-error path so the FSM driver translates it into a
		// terminal-failed StageResult (and BudgetReleaseAllowed correctly
		// skips a release: no reservation was ever placed).
		w.claimFail(ctx, scope, token, "SAFETY_BLOCKED")
		return StageResult{}, generation.Terminal("SAFETY_BLOCKED", reasonOr(verdict.ReasonCode, "input moderation blocked the prompt"))

	case safety.DecisionReview:
		// REVIEW also short-circuits: the prompt sits in a hold state with
		// no cost reservation. Distinct claim-fail code so review queues
		// can filter on it without inspecting the verdict store; the
		// terminal Error still reports SAFETY_BLOCKED so callers don't
		// need to enumerate every safety reason code at the FSM boundary.
		w.claimFail(ctx, scope, token, "SAFETY_BLOCKED_PENDING_REVIEW")
		return StageResult{}, generation.Terminal("SAFETY_BLOCKED", reasonOr(verdict.ReasonCode, "input moderation pending review"))
	}

	// Unknown decision string from the moderator. Surface as transient so
	// the moderator can be re-called rather than terminating a job on a
	// classifier bug.
	w.claimAbandon(ctx, scope, token)
	return StageResult{}, generation.Transient("INPUT_MODERATION_UNKNOWN_VERDICT", string(verdict.Decision))
}

// moderate runs the wired Moderator. With no Moderator wired (test path)
// returns a synthetic PASS verdict so the FSM stays driveable without a
// classifier; production wiring populates w.Moderator at bootstrap.
func (w *Workflow) moderate(ctx context.Context, in safetyapp.ModerateInput) (safety.Verdict, error) {
	if w.Moderator == nil {
		return safety.Verdict{
			Layer:         in.Layer,
			Decision:      safety.DecisionPass,
			Provider:      "permissive-default",
			Model:         "n/a",
			PolicyVersion: "permissive-v0",
			CreatedAt:     w.now(),
		}, nil
	}
	return w.Moderator.Moderate(ctx, in)
}

// recordModerationEvent writes the moderation decision through the standalone
// Recorder when wired. The audit-store error is intentionally swallowed: a
// flake there must never cause a moderation re-call, because the moderator's
// verdict is the authoritative record and the audit row is a downstream
// observability row. The recorder collapses duplicates internally so retries
// are transparent.
func (w *Workflow) recordModerationEvent(ctx context.Context, ev audit.Event, ts time.Time) {
	if w.AuditRecorder == nil {
		return
	}
	if !ts.IsZero() {
		ev.CreatedAt = ts
	}
	_ = w.AuditRecorder.Record(ctx, ev)
}

// reasonOr returns a fallback when the moderator did not attach a reason
// code. Keeps the terminal error message human-readable for operators
// scanning the failure ledger.
func reasonOr(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}

// emitSafetyDecision increments safety.decisions_total once per moderation
// verdict. The category label is the highest-scoring entry in
// verdict.Categories so dashboards can break down rejections by what the
// classifier actually flagged; `none` covers the PASS path and the no-
// signal case so the label cardinality stays closed.
func (w *Workflow) emitSafetyDecision(ctx context.Context, v safety.Verdict) {
	category := topCategory(v.Categories)
	w.Instruments.SafetyDecisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("layer", string(v.Layer)),
		attribute.String("decision", string(v.Decision)),
		attribute.String("category", category),
	))
}

// topCategory returns the highest-scored category from a verdict. Returning
// the bare key (rather than the score) keeps the metric label cardinality
// bounded by the policy's category vocabulary.
func topCategory(scores map[string]float64) string {
	if len(scores) == 0 {
		return "none"
	}
	var (
		topKey   string
		topScore float64
		seen     bool
	)
	for k, v := range scores {
		if !seen || v > topScore {
			topKey = k
			topScore = v
			seen = true
		}
	}
	if topKey == "" {
		return "none"
	}
	return topKey
}
