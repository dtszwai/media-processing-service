package generation

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
)

// stageQuotaReserve gates the FSM on the tenant cost reservoir. When a
// QuotaLedger is wired, the aggregate Reserve rides in the same
// TransactWrite as the per-reservation ledger Put (committed by
// AdvanceStageAndEnqueue), so the standalone Reserve call is only used
// when the ledger is absent (test path).
func (w *Workflow) stageQuotaReserve(ctx context.Context, job *generation.Job) (StageResult, error) {
	cost := DefaultCostMicroUSD(job.OutputType)
	period := quota.PeriodDaily(w.now())
	if w.QuotaReserver != nil {
		if w.QuotaLedger == nil {
			// Reserve bootstraps the aggregate row itself and gates on
			// available >= cost before the provider is ever called.
			granted, _, err := w.QuotaReserver.Reserve(ctx, job.TenantID, period, cost)
			if err != nil {
				return StageResult{}, err
			}
			if !granted {
				return StageResult{}, generation.Terminal("BUDGET_EXHAUSTED", "tenant daily budget exhausted; provider not called")
			}
		} else {
			// LedgerOp path: the aggregate decrement rides in the txn as a
			// conditional Update on `available >= :n`, which requires the row
			// to already exist. Bootstrap once before assembling the txn.
			if err := w.QuotaReserver.Ensure(ctx, job.TenantID, period); err != nil {
				return StageResult{}, err
			}
		}
	}
	result := w.nextStageResult(ctx, job, generation.StagePromptPrepare, generation.ResourceFast)
	result.BudgetDate = period
	result.BudgetMicroUSD = cost
	if w.QuotaLedger != nil {
		op := w.QuotaLedger.LedgerPutReserved(job.TenantID, job.ID, period, cost, job.Attempts+1)
		result.LedgerOp = &op
	}
	return result, nil
}
