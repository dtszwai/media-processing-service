package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Reserve atomically subtracts amount from the aggregate `available` and
// records a RESERVED ledger row. Both ops ride in a single TransactWrite so
// the per-reservation row only lands when the aggregate decrement commits.
// Returns ErrQuotaExhausted when the conditional update rejects (available
// < amount or state != OPEN); callers map that to a terminal job error.
func (r *Repo) Reserve(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string, reservation quota.Reservation) error {
	if reservation.ID == "" {
		return errors.New("quota: reservation.ID required")
	}
	if scopeID == "" {
		return errors.New("quota: scope_id required")
	}
	if reservation.Amount <= 0 {
		// Zero-amount reservations are a no-op rather than an error so
		// callers (e.g. free-tier outputs) don't need a branch.
		return nil
	}
	now := r.Now().UTC()
	plan := kv.TxPlan{
		Name: "quota.reserve",
		Ops: []kv.NamedTxOp{
			{Name: OpAggregateReserve, Op: r.aggregateReserveOp(scope, scopeID, metric, period, reservation.Amount, now)},
			{Name: OpLedgerReserve, Op: r.ledgerPutOp(scope, scopeID, metric, period, reservation, now)},
		},
	}
	if err := plan.Execute(ctx, r.KV); err != nil {
		var txErr kv.TxnError
		if errors.As(err, &txErr) {
			if ok, gerr := r.ledgerReplayMatches(ctx, scope, scopeID, metric, period, reservation.ID, reservation.Amount, quota.ReservationReserved, quota.ReservationCommitted); gerr != nil {
				return fmt.Errorf("quota: inspect duplicate reservation: %w", gerr)
			} else if ok {
				return nil
			}
			// AGG cancellation = aggregate condition failed (available < amount
			// OR state != OPEN). LEDGER cancellation = duplicate reservation
			// id (handler retried the same Reserve). Both surface to the
			// caller as ErrQuotaExhausted for the former and a wrapped
			// kv.ErrConditionFailed for the latter so handlers can branch.
			if name, ok := kv.ClassifyByName(plan, txErr); ok {
				switch name {
				case OpAggregateReserve:
					return ErrQuotaExhausted
				case OpLedgerReserve:
					return fmt.Errorf("quota: reservation already exists: %w", kv.ErrConditionFailed)
				}
			}
		}
		return fmt.Errorf("quota: reserve: %w", err)
	}
	return nil
}

// Commit moves amount from reserved to committed on the aggregate row and
// flips the per-reservation ledger row to COMMITTED. Both ride in one
// TransactWrite. Conditional on ledger.state = RESERVED so a double-commit
// cancels rather than over-charging.
func (r *Repo) Commit(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	now := r.Now().UTC()
	plan := kv.TxPlan{
		Name: "quota.commit",
		Ops: []kv.NamedTxOp{
			{Name: OpAggregateCommit, Op: r.aggregateCommitOp(scope, scopeID, metric, period, amount, now)},
			{Name: OpLedgerCommit, Op: r.ledgerCommitOp(scope, scopeID, metric, period, reservationID, now)},
		},
	}
	if err := plan.Execute(ctx, r.KV); err != nil {
		if ok, gerr := r.ledgerReplayMatches(ctx, scope, scopeID, metric, period, reservationID, amount, quota.ReservationCommitted); gerr != nil {
			return fmt.Errorf("quota: inspect committed reservation: %w", gerr)
		} else if ok {
			return nil
		}
		return fmt.Errorf("quota: commit: %w", err)
	}
	return nil
}

// Release moves amount from reserved back to available on the aggregate row
// and flips the per-reservation ledger row to RELEASED. Conditional on
// ledger.state = RESERVED so release-after-commit cancels.
func (r *Repo) Release(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	now := r.Now().UTC()
	plan := kv.TxPlan{
		Name: "quota.release",
		Ops: []kv.NamedTxOp{
			{Name: OpAggregateRelease, Op: r.aggregateReleaseOp(scope, scopeID, metric, period, amount, now)},
			{Name: OpLedgerRelease, Op: r.ledgerReleaseOp(scope, scopeID, metric, period, reservationID, now)},
		},
	}
	if err := plan.Execute(ctx, r.KV); err != nil {
		if ok, gerr := r.ledgerReplayMatches(ctx, scope, scopeID, metric, period, reservationID, amount, quota.ReservationReleased); gerr != nil {
			return fmt.Errorf("quota: inspect released reservation: %w", gerr)
		} else if ok {
			return nil
		}
		return fmt.Errorf("quota: release: %w", err)
	}
	return nil
}
