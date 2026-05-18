package generation

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func (w *Workflow) resolveTransition(ctx context.Context, job *generation.Job, result StageResult) (StageResult, error) {
	if result.Outcome == "" {
		return StageResult{}, generation.Terminal("UNKNOWN_STAGE_TRANSITION", string(job.CurrentStage)+" <empty>")
	}
	if result.NextStage != "" || len(result.OutboxBody) > 0 || result.ResourceClass != "" {
		return StageResult{}, generation.Terminal("MIXED_STAGE_ROUTING", "stage result set both semantic outcome and direct routing fields")
	}

	switch result.Outcome {
	case OutcomeTransientRetry:
		return w.routedStageResult(ctx, job, result, job.CurrentStage, stageWorkClass(job)), nil

	case OutcomeModerationPassed:
		switch job.CurrentStage {
		case generation.StageInputModeration:
			return w.routedStageResult(ctx, job, result, generation.StageCostReserve, generation.ResourceFast), nil
		case generation.StageOutputModeration:
			return w.routedStageResult(ctx, job, result, generation.StageDisclosurePostprocess, resourceClassForPostprocess(job)), nil
		}

	case OutcomeBudgetReserved:
		if job.CurrentStage == generation.StageCostReserve {
			resolved := w.routedStageResult(ctx, job, result, generation.StagePromptPrepare, generation.ResourceFast)
			if w.QuotaLedger != nil {
				op := w.QuotaLedger.LedgerPutReserved(job.TenantID, job.ID, result.BudgetDate, result.BudgetMicroUSD, job.Attempts+1)
				resolved.LedgerOp = &op
			}
			return resolved, nil
		}

	case OutcomePromptPrepared:
		if job.CurrentStage == generation.StagePromptPrepare {
			return w.routedStageResult(ctx, job, result, generation.StageProviderSubmit, generation.ResourceProvider), nil
		}

	case OutcomeProviderSubmittedAsync:
		if job.CurrentStage == generation.StageProviderSubmit {
			return w.routedStageResult(ctx, job, result, generation.StageProviderWait, generation.ResourcePoll), nil
		}

	case OutcomeArtifactStaged:
		switch job.CurrentStage {
		case generation.StageProviderSubmit, generation.StageProviderWait:
			resolved := w.routedStageResult(ctx, job, result, generation.StageOutputModeration, generation.ResourceFast)
			if w.QuotaLedger != nil {
				op := w.QuotaLedger.LedgerUpdateCommitted(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
				resolved.LedgerOp = &op
			}
			return resolved, nil
		}

	case OutcomePollPending:
		if job.CurrentStage == generation.StageProviderWait {
			return w.routedStageResult(ctx, job, result, generation.StageProviderWait, generation.ResourcePoll), nil
		}

	case OutcomeProviderJobFailed:
		if job.CurrentStage == generation.StageProviderWait {
			resolved := result
			resolved.NextStage = StageTerminal
			resolved.TerminalError = &generation.Error{
				Code:     "PROVIDER_JOB_FAILED",
				Message:  "async provider reported job failure",
				Terminal: true,
			}
			if BudgetReleaseAllowed(job.CurrentStage) {
				w.attachLedgerRelease(ctx, job, &resolved)
			}
			return resolved, nil
		}

	case OutcomeDisclosureComplete:
		if job.CurrentStage == generation.StageDisclosurePostprocess {
			return w.routedStageResult(ctx, job, result, generation.StagePublish, generation.ResourceFast), nil
		}

	case OutcomePublished:
		if job.CurrentStage == generation.StagePublish {
			resolved := result
			resolved.NextStage = StageTerminal
			if resolved.CompletedAt == nil {
				now := w.now()
				resolved.CompletedAt = &now
			}
			return resolved, nil
		}
	}

	return StageResult{}, generation.Terminal("UNKNOWN_STAGE_TRANSITION", string(job.CurrentStage)+" "+string(result.Outcome))
}

func (w *Workflow) routedStageResult(ctx context.Context, job *generation.Job, result StageResult, next generation.Stage, class generation.ResourceClass) StageResult {
	body, _ := MarshalStageMessage(job.TenantID, job.ID, next, job.StageVersion+1, class, TraceparentFromContext(ctx))
	result.NextStage = next
	result.OutboxBody = body
	result.ResourceClass = class
	return result
}
