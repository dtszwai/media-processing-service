package generation

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// BudgetReleaseAllowed reports whether a terminal transition can release a
// reserved budget entry because provider cost has not been committed yet.
func BudgetReleaseAllowed(stage generation.Stage) bool {
	switch stage {
	case generation.StageCostReserve, generation.StagePromptPrepare,
		generation.StageProviderSubmit, generation.StageProviderWait:
		return true
	}
	return false
}

// attachLedgerRelease appends a ledger Release op to result when QuotaLedger
// is wired and the job has a recorded reservation (BudgetDate non-empty —
// the field name preserves the on-disk attribute; the value is the period
// the reservation was staked under). When QuotaLedger is absent, falls back
// to a standalone QuotaReserver.Release so the aggregate row stays
// consistent.
func (w *Workflow) attachLedgerRelease(ctx context.Context, job *generation.Job, result *StageResult) {
	if job.BudgetDate == "" {
		return
	}
	if w.QuotaLedger != nil {
		op := w.QuotaLedger.LedgerUpdateReleased(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
		result.LedgerOp = &op
		return
	}
	// Fallback: no ledger wired; use standalone aggregate Release so the
	// counter is not stranded.
	if w.QuotaReserver != nil {
		_ = w.QuotaReserver.Release(ctx, job.TenantID, job.BudgetDate, DefaultCostMicroUSD(job.OutputType))
	}
}

func (w *Workflow) nextStageResult(ctx context.Context, job *generation.Job, next generation.Stage, class generation.ResourceClass) StageResult {
	body, _ := MarshalStageMessage(job.TenantID, job.ID, next, job.StageVersion+1, class, TraceparentFromContext(ctx))
	return StageResult{
		NextStage:     next,
		OutboxBody:    body,
		ResourceClass: class,
	}
}

func resourceClassForPostprocess(job *generation.Job) generation.ResourceClass {
	switch job.OutputType {
	case generation.OutputImage:
		return generation.ResourceImageProcess
	default:
		return generation.ResourceFast
	}
}
