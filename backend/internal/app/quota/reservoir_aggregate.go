package quota

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Ensure materializes the aggregate Reservoir row for the (scope, metric,
// period) tuple if one is not already there. Conditional on
// attribute_not_exists(PK) so concurrent callers collapse to a single live
// row — `cap`, `policy_id`, and `policy_version` only land on the first
// write so a later Ensure with a different cap is intentionally a no-op
// (cap changes go through a dedicated policy-rotation path that also emits
// a quota.cap.changed audit row).
func (r *Repo) Ensure(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string, capN int64, policyID string, policyVersion int64) error {
	if scopeID == "" {
		return errors.New("quota: scope_id required")
	}
	if capN < 0 {
		return errors.New("quota: cap must be non-negative")
	}
	now := r.Now().UTC()
	// 1y TTL matches the audit retention so a stale reservoir from a closed
	// tenant doesn't linger forever in a hot table partition.
	ttl := now.Add(365 * 24 * time.Hour).Unix()
	item := map[string]any{
		"PK":             ReservoirPK(scope, scopeID, metric, period),
		"SK":             AggSK,
		"scope_type":     string(scope),
		"scope_id":       scopeID,
		"metric":         string(metric),
		"period":         period,
		"cap":            capN,
		"available":      capN,
		"reserved":       int64(0),
		"committed":      int64(0),
		"released":       int64(0),
		"state":          string(quota.ReservoirOpen),
		"policy_id":      policyID,
		"policy_version": policyVersion,
		"created_at":     now.Format(time.RFC3339Nano),
		"updated_at":     now.Format(time.RFC3339Nano),
		"ttl_epoch":      ttl,
	}
	err := r.KV.Put(ctx, item, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK)",
	})
	if err == nil || errors.Is(err, kv.ErrConditionFailed) {
		return nil
	}
	return fmt.Errorf("quota: ensure: %w", err)
}

// Get reads the aggregate row. Returns kv.ErrNotFound when no row exists.
func (r *Repo) Get(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string) (*quota.Reservoir, error) {
	var row aggRow
	if err := r.KV.Get(ctx, AggKey(scope, scopeID, metric, period), &row); err != nil {
		return nil, err
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return &quota.Reservoir{
		ScopeType:     quota.ScopeType(row.ScopeType),
		ScopeID:       row.ScopeID,
		Metric:        quota.Metric(row.Metric),
		Period:        row.Period,
		Cap:           row.Cap,
		Available:     row.Available,
		Reserved:      row.Reserved,
		Committed:     row.Committed,
		Released:      row.Released,
		State:         quota.ReservoirState(row.State),
		PolicyID:      row.PolicyID,
		PolicyVersion: row.PolicyVersion,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}

// OverrideCap changes a live reservoir cap while preserving the invariant
// that available capacity cannot go negative. The update is conditional on
// the cap and updated_at values just read, so concurrent operator changes
// fail rather than losing one override.
func (r *Repo) OverrideCap(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period string, newCap int64) (CapOverride, error) {
	if newCap < 0 {
		return CapOverride{}, errors.New("quota: cap must be non-negative")
	}
	current, err := r.Get(ctx, scope, scopeID, metric, period)
	if err != nil {
		return CapOverride{}, err
	}
	delta := newCap - current.Cap
	now := r.Now().UTC()
	vals := kv.Values{
		":new_cap":     newCap,
		":delta":       delta,
		":old_cap":     current.Cap,
		":old_updated": current.UpdatedAt.Format(time.RFC3339Nano),
		":now":         now.Format(time.RFC3339Nano),
		":one":         int64(1),
	}
	condition := "cap = :old_cap AND updated_at = :old_updated"
	if delta < 0 {
		vals[":shortfall"] = -delta
		condition += " AND available >= :shortfall"
	}
	out, err := r.KV.UpdateReturning(ctx, kv.UpdateOp{
		Key:                       AggKey(scope, scopeID, metric, period),
		UpdateExpression:          "SET cap = :new_cap, available = available + :delta, policy_version = policy_version + :one, updated_at = :now",
		ConditionExpression:       condition,
		ExpressionAttributeValues: vals,
	})
	if err != nil {
		return CapOverride{}, fmt.Errorf("quota: override cap: %w", err)
	}
	return CapOverride{
		PreviousCap:      current.Cap,
		NewCap:           newCap,
		ReservoirVersion: readInt64(out.Attributes["policy_version"]),
	}, nil
}

// aggRow is the dynamodbav shape of the Reservoir aggregate row. Used by
// Get; the writes go through map[string]any so each transition can ship
// minimal SETs.
type aggRow struct {
	ScopeType     string `dynamodbav:"scope_type"`
	ScopeID       string `dynamodbav:"scope_id"`
	Metric        string `dynamodbav:"metric"`
	Period        string `dynamodbav:"period"`
	Cap           int64  `dynamodbav:"cap"`
	Available     int64  `dynamodbav:"available"`
	Reserved      int64  `dynamodbav:"reserved"`
	Committed     int64  `dynamodbav:"committed"`
	Released      int64  `dynamodbav:"released"`
	State         string `dynamodbav:"state"`
	PolicyID      string `dynamodbav:"policy_id"`
	PolicyVersion int64  `dynamodbav:"policy_version"`
	CreatedAt     string `dynamodbav:"created_at"`
	UpdatedAt     string `dynamodbav:"updated_at"`
}

func (r *Repo) aggregateReserveOp(scope quota.ScopeType, scopeID string, metric quota.Metric, period string, amount int64, now time.Time) kv.WriteOp {
	return kv.WriteOp{Update: &kv.UpdateOp{
		Key:                 AggKey(scope, scopeID, metric, period),
		UpdateExpression:    "SET available = available - :n, reserved = reserved + :n, updated_at = :now",
		ConditionExpression: "available >= :n AND #state = :open",
		ExpressionAttributeNames: kv.Names{
			"#state": "state",
		},
		ExpressionAttributeValues: kv.Values{
			":n":    amount,
			":now":  now.Format(time.RFC3339Nano),
			":open": string(quota.ReservoirOpen),
		},
	}}
}

func (r *Repo) aggregateCommitOp(scope quota.ScopeType, scopeID string, metric quota.Metric, period string, amount int64, now time.Time) kv.WriteOp {
	return kv.WriteOp{Update: &kv.UpdateOp{
		Key:                 AggKey(scope, scopeID, metric, period),
		UpdateExpression:    "SET reserved = reserved - :n, committed = committed + :n, updated_at = :now",
		ConditionExpression: "reserved >= :n",
		ExpressionAttributeValues: kv.Values{
			":n":   amount,
			":now": now.Format(time.RFC3339Nano),
		},
	}}
}

func (r *Repo) aggregateReleaseOp(scope quota.ScopeType, scopeID string, metric quota.Metric, period string, amount int64, now time.Time) kv.WriteOp {
	return kv.WriteOp{Update: &kv.UpdateOp{
		Key:                 AggKey(scope, scopeID, metric, period),
		UpdateExpression:    "SET reserved = reserved - :n, available = available + :n, released = released + :n, updated_at = :now",
		ConditionExpression: "reserved >= :n",
		ExpressionAttributeValues: kv.Values{
			":n":   amount,
			":now": now.Format(time.RFC3339Nano),
		},
	}}
}

func readInt64(v any) int64 {
	switch n := v.(type) {
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
