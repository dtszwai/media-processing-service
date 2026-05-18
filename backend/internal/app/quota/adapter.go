package quota

import (
	"context"
	"errors"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// WorkflowAdapter implements generation.QuotaReserver and
// generation.QuotaLedger atop a *Repo. The adapter is the only place that
// binds the FSM's tenant-cost vocabulary to the Reservoir scope/metric
// taxonomy — the workflow itself stays scope-agnostic so swapping the
// adapter (e.g. for an API-key cost reservoir) doesn't ripple through the
// stage handlers.
//
// Reserve / Commit / Release on the aggregate row are standalone updates
// (used as the budget gate when no ledger ships in the transaction). The
// per-job ledger row is built as kv.WriteOps that ride with
// AdvanceStageAndEnqueue so the ledger state flips atomically with the
// stage transition.
type WorkflowAdapter struct {
	repo       *Repo
	defaultCap int64
	now        func() time.Time
	// policyID/policyVersion identify which cap policy the aggregate row
	// was opened under. For this slice the policy is static; future
	// slices that introduce per-scope-type policies switch on these.
	policyID      string
	policyVersion int64
}

// NewWorkflowAdapter constructs the adapter atop a Repo with the given
// per-tenant daily cost cap (micro-USD).
func NewWorkflowAdapter(r *Repo, defaultCapMicroUSD int64) *WorkflowAdapter {
	return &WorkflowAdapter{
		repo:          r,
		defaultCap:    defaultCapMicroUSD,
		now:           func() time.Time { return time.Now().UTC() },
		policyID:      "tenant_default_v1",
		policyVersion: 1,
	}
}

// Ensure materializes the tenant-cost reservoir for period if missing.
// Idempotent — concurrent calls collapse to the first writer.
func (a *WorkflowAdapter) Ensure(ctx context.Context, tenantID, period string) error {
	return a.repo.Ensure(ctx, quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, a.defaultCap, a.policyID, a.policyVersion)
}

// Reserve is the test-path entry: a standalone aggregate decrement that
// returns whether the gate granted the spend. The production FSM uses
// LedgerPutReserved so the per-reservation row lands in the same txn.
func (a *WorkflowAdapter) Reserve(ctx context.Context, tenantID, period string, microUSD int64) (bool, int64, error) {
	if err := a.Ensure(ctx, tenantID, period); err != nil {
		return false, 0, err
	}
	return a.repo.StandaloneReserve(ctx, quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, microUSD)
}

// HasCapacity is the read-only submit-time hint. It never creates a reservoir
// or mutates available capacity; COST_RESERVE remains the authoritative gate.
func (a *WorkflowAdapter) HasCapacity(ctx context.Context, tenantID, period string, requiredMicroUSD int64) (bool, int64, error) {
	if tenantID == "" {
		return false, 0, errors.New("quota hint: tenant_id required")
	}
	res, err := a.repo.Get(ctx, quota.ScopeTenant, tenantID, quota.CostMicroUSD, period)
	if errors.Is(err, kv.ErrNotFound) {
		return a.defaultCap >= requiredMicroUSD, a.defaultCap, nil
	}
	if err != nil {
		return false, 0, err
	}
	if res.State != quota.ReservoirOpen {
		return false, res.Available, nil
	}
	return res.Available >= requiredMicroUSD, res.Available, nil
}

// Commit is the test-path standalone commit. Mirrors Reserve.
func (a *WorkflowAdapter) Commit(ctx context.Context, tenantID, period string, microUSD int64) error {
	return a.repo.StandaloneCommit(ctx, quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, microUSD)
}

// Release is the test-path standalone release. Mirrors Reserve.
func (a *WorkflowAdapter) Release(ctx context.Context, tenantID, period string, microUSD int64) error {
	return a.repo.StandaloneRelease(ctx, quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, microUSD)
}

// LedgerPutReserved returns the per-job RESERVED Put plus the aggregate
// reserved-increment Update, ordered aggregate-first/ledger-last so
// AdvanceStageAndEnqueue can tag each slot with the canonical TxOpName.
func (a *WorkflowAdapter) LedgerPutReserved(tenantID, jobID, period string, microUSD int64, attempt int) genapp.LedgerOp {
	now := a.now().UTC()
	res := quota.Reservation{
		ID:             jobID,
		JobID:          jobID,
		Amount:         microUSD,
		State:          quota.ReservationReserved,
		Reason:         "GENERATION_ESTIMATE",
		PricingVersion: a.policyID,
		CreatedAt:      now,
		ReservedAt:     now,
	}
	return genapp.LedgerOp{Items: []kv.WriteOp{
		a.aggregateReserveSlot(tenantID, period, microUSD, now),
		a.ledgerReserveSlot(tenantID, period, res, now, attempt),
	}}
}

// LedgerUpdateCommitted returns the per-job COMMITTED Update plus the
// aggregate committed-increment Update.
func (a *WorkflowAdapter) LedgerUpdateCommitted(tenantID, jobID, period string, microUSD int64) genapp.LedgerOp {
	now := a.now().UTC()
	return genapp.LedgerOp{Items: []kv.WriteOp{
		a.aggregateCommitSlot(tenantID, period, microUSD, now),
		a.ledgerCommitSlot(tenantID, period, jobID, now),
	}}
}

// LedgerUpdateReleased returns the per-job RELEASED Update plus the
// aggregate release Update.
func (a *WorkflowAdapter) LedgerUpdateReleased(tenantID, jobID, period string, microUSD int64) genapp.LedgerOp {
	now := a.now().UTC()
	return genapp.LedgerOp{Items: []kv.WriteOp{
		a.aggregateReleaseSlot(tenantID, period, microUSD, now),
		a.ledgerReleaseSlot(tenantID, period, jobID, now),
	}}
}

// Repo exposes the underlying repo so test code can construct it directly.
// Returning a copy of the pointer is fine — *Repo is value-stable.
func (a *WorkflowAdapter) Repo() *Repo { return a.repo }

func (a *WorkflowAdapter) aggregateReserveSlot(tenantID, period string, microUSD int64, now time.Time) kv.WriteOp {
	return a.repo.aggregateReserveOp(quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, microUSD, now)
}

func (a *WorkflowAdapter) aggregateCommitSlot(tenantID, period string, microUSD int64, now time.Time) kv.WriteOp {
	return a.repo.aggregateCommitOp(quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, microUSD, now)
}

func (a *WorkflowAdapter) aggregateReleaseSlot(tenantID, period string, microUSD int64, now time.Time) kv.WriteOp {
	return a.repo.aggregateReleaseOp(quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, microUSD, now)
}

func (a *WorkflowAdapter) ledgerReserveSlot(tenantID, period string, res quota.Reservation, now time.Time, attempt int) kv.WriteOp {
	op := a.repo.ledgerPutOp(quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, res, now)
	// Riding inside AdvanceStageAndEnqueue: tag the row with the attempt
	// number so post-mortems can correlate the FSM retry count with the
	// reservation row. Mutating the item before the txn submits is safe
	// — kv.WriteOp.Put.Item is the only path that observes it.
	if put := op.Put; put != nil {
		if m, ok := put.Item.(map[string]any); ok {
			m["attempt"] = attempt
		}
	}
	return op
}

func (a *WorkflowAdapter) ledgerCommitSlot(tenantID, period, jobID string, now time.Time) kv.WriteOp {
	return a.repo.ledgerCommitOp(quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, jobID, now)
}

func (a *WorkflowAdapter) ledgerReleaseSlot(tenantID, period, jobID string, now time.Time) kv.WriteOp {
	return a.repo.ledgerReleaseOp(quota.ScopeTenant, tenantID, quota.CostMicroUSD, period, jobID, now)
}

// Reserve / Commit / Release signatures satisfy genapp.QuotaReserver — pin
// the assertion here so a future signature drift lights up at this package
// rather than at the call site.
var _ interface {
	Ensure(ctx context.Context, tenantID, period string) error
	HasCapacity(ctx context.Context, tenantID, period string, requiredMicroUSD int64) (bool, int64, error)
	Reserve(ctx context.Context, tenantID, period string, microUSD int64) (bool, int64, error)
	Commit(ctx context.Context, tenantID, period string, microUSD int64) error
	Release(ctx context.Context, tenantID, period string, microUSD int64) error
} = (*WorkflowAdapter)(nil)
