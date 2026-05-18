package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// StandaloneReserve runs only the aggregate-row decrement (no ledger row) and
// reports remaining capacity. Used when no ledger is wired (test path);
// production assembles the per-reservation ledger row into the same
// TransactWrite via WorkflowAdapter.LedgerPutReserved.
func (r *Repo) StandaloneReserve(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string, amount int64) (granted bool, remaining int64, err error) {
	if scopeID == "" {
		return false, 0, errors.New("quota: scope_id required")
	}
	if amount <= 0 {
		return true, 0, nil
	}
	op := r.aggregateReserveOp(scope, scopeID, metric, period, amount, r.Now().UTC())
	out, uerr := r.KV.UpdateReturning(ctx, *op.Update)
	if uerr != nil {
		if errors.Is(uerr, kv.ErrConditionFailed) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("quota: standalone reserve: %w", uerr)
	}
	return true, readInt64(out.Attributes["available"]), nil
}

// StandaloneCommit is the no-ledger commit. Mirrors StandaloneReserve.
func (r *Repo) StandaloneCommit(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	op := r.aggregateCommitOp(scope, scopeID, metric, period, amount, r.Now().UTC())
	if err := r.KV.Update(ctx, *op.Update); err != nil {
		return fmt.Errorf("quota: standalone commit: %w", err)
	}
	return nil
}

// StandaloneRelease is the no-ledger release. Mirrors StandaloneReserve.
func (r *Repo) StandaloneRelease(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	op := r.aggregateReleaseOp(scope, scopeID, metric, period, amount, r.Now().UTC())
	return r.KV.Update(ctx, *op.Update)
}
