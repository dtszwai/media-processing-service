package quota

import (
	"fmt"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Canonical op names a TxPlan attaches to slots whose cancellation maps to a
// RetryClass. AdvanceStageAndEnqueue uses the *TenantQuota names verbatim
// when threading a stage-bound quota mutation; the standalone Reserve /
// Commit / Release ops in this package use the matching per-transition
// names. Centralizing the names means the classifier survives reordering of
// the underlying transaction slots.
const (
	OpAdvanceJobStage      kv.TxOpName = "advance_job_stage"
	OpPutOutboxNextStage   kv.TxOpName = "put_outbox_next_stage"
	OpAggregateTenantQuota kv.TxOpName = "aggregate_tenant_quota"
	OpLedgerTenantQuota    kv.TxOpName = "ledger_tenant_quota"
	OpAggregateReserve     kv.TxOpName = "aggregate_reserve"
	OpLedgerReserve        kv.TxOpName = "ledger_reserve"
	OpAggregateCommit      kv.TxOpName = "aggregate_commit"
	OpLedgerCommit         kv.TxOpName = "ledger_commit"
	OpAggregateRelease     kv.TxOpName = "aggregate_release"
	OpLedgerRelease        kv.TxOpName = "ledger_release"
)

// RetryClass classifies a TransactWrite cancellation so callers take the
// right recovery action without inspecting raw cancellation reasons.
type RetryClass int

const (
	// RetryReplay — another worker already advanced this job's stage. Skip.
	RetryReplay RetryClass = iota
	// RetryConflict — reservation state condition failed (double-commit,
	// duplicate ledger row, or release-after-charge). Alert and skip.
	RetryConflict
	// RetryExhausted — aggregate reservoir row ran out of available
	// capacity. Fail the job with a terminal BUDGET_EXHAUSTED error.
	RetryExhausted
)

func (c RetryClass) String() string {
	switch c {
	case RetryReplay:
		return "REPLAY"
	case RetryConflict:
		return "CONFLICT"
	case RetryExhausted:
		return "EXHAUSTED"
	default:
		return fmt.Sprintf("RetryClass(%d)", int(c))
	}
}

// ClassifyTxnError maps the cancelling op in plan back to a RetryClass.
// Priority is encoded in the per-op name semantics, not in slot position:
// ledger-row conflicts beat aggregate exhaustion which beats stage-condition
// replay because callers care about the most-specific signal, and the op
// name uniquely identifies it.
func ClassifyTxnError(plan kv.TxPlan, err kv.TxnError) RetryClass {
	if err == nil {
		return RetryReplay
	}
	name, found := kv.ClassifyByName(plan, err)
	if !found {
		return RetryReplay
	}
	switch name {
	case OpLedgerTenantQuota, OpLedgerReserve, OpLedgerCommit, OpLedgerRelease, OpAggregateCommit, OpAggregateRelease:
		return RetryConflict
	case OpAggregateTenantQuota, OpAggregateReserve:
		return RetryExhausted
	default:
		return RetryReplay
	}
}
