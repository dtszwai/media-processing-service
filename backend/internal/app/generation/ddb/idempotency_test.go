package ddb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestIdempotency_ReclaimDoesNotResurrectCompletedClaim(t *testing.T) {
	store := NewIdempotency(newIdemKV())
	store.Now = fixedIdemClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	outcome, token, err := store.Claim(ctx, "scope-complete", "hash", time.Minute)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if outcome != idempotency.OutcomeNew {
		t.Fatalf("outcome = %s, want NEW", outcome)
	}
	if err := store.Complete(ctx, "scope-complete", token, "ref"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	store.Now = fixedIdemClock(time.Date(2026, 5, 16, 9, 2, 0, 0, time.UTC))
	if _, err := store.Reclaim(ctx, "scope-complete", time.Minute); !errors.Is(err, kv.ErrConditionFailed) {
		t.Fatalf("Reclaim completed err = %v, want ErrConditionFailed", err)
	}
	if err := store.Fail(ctx, "scope-complete", token, "LATE_FAIL"); !errors.Is(err, kv.ErrConditionFailed) {
		t.Fatalf("Fail completed err = %v, want ErrConditionFailed", err)
	}
}

func TestIdempotency_ReclaimDoesNotResurrectFailedClaim(t *testing.T) {
	store := NewIdempotency(newIdemKV())
	store.Now = fixedIdemClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	_, token, err := store.Claim(ctx, "scope-failed", "hash", time.Minute)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := store.Fail(ctx, "scope-failed", token, "PROVIDER_FAILED"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	store.Now = fixedIdemClock(time.Date(2026, 5, 16, 9, 2, 0, 0, time.UTC))
	if _, err := store.Reclaim(ctx, "scope-failed", time.Minute); !errors.Is(err, kv.ErrConditionFailed) {
		t.Fatalf("Reclaim failed err = %v, want ErrConditionFailed", err)
	}
	if err := store.Complete(ctx, "scope-failed", token, "ref"); !errors.Is(err, kv.ErrConditionFailed) {
		t.Fatalf("Complete failed err = %v, want ErrConditionFailed", err)
	}
}

type idemKV struct {
	rows map[string]map[string]any
}

func newIdemKV() *idemKV {
	return &idemKV{rows: map[string]map[string]any{}}
}

func fixedIdemClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func (f *idemKV) Put(_ context.Context, item kv.Item, opts kv.PutOptions) error {
	row, ok := idemRowToMap(item)
	if !ok {
		return errors.New("idemKV: unsupported Put item")
	}
	key := row["PK"].(string) + "\x00" + row["SK"].(string)
	if strings.Contains(opts.ConditionExpression, "attribute_not_exists(PK)") {
		if _, exists := f.rows[key]; exists {
			return kv.ErrConditionFailed
		}
	}
	f.rows[key] = cloneIdemRow(row)
	return nil
}

func (f *idemKV) Get(_ context.Context, key kv.Key, out any) error {
	row, ok := f.rows[key.PK+"\x00"+key.SK]
	if !ok {
		return kv.ErrNotFound
	}
	switch dst := out.(type) {
	case *idemRow:
		dst.PK, _ = row["PK"].(string)
		dst.SK, _ = row["SK"].(string)
		dst.InputHash, _ = row["input_hash"].(string)
		dst.Status, _ = row["status"].(string)
		dst.Result, _ = row["result"].(string)
		dst.ErrorCode, _ = row["error_code"].(string)
		dst.ClaimToken, _ = row["claim_token"].(string)
		dst.LeaseUntil, _ = row["lease_until"].(int64)
		dst.Attempts, _ = row["attempts"].(int)
		dst.TTLEpoch, _ = row["ttl_epoch"].(int64)
		dst.CreatedAt, _ = row["created_at"].(string)
		dst.UpdatedAt, _ = row["updated_at"].(string)
	default:
		return errors.New("idemKV: unsupported Get target")
	}
	return nil
}

func (f *idemKV) Update(_ context.Context, op kv.UpdateOp) error {
	key := op.Key.PK + "\x00" + op.Key.SK
	row, exists := f.rows[key]
	if !exists || !idemConditionOK(row, op.ConditionExpression, op.ExpressionAttributeNames, op.ExpressionAttributeValues) {
		return kv.ErrConditionFailed
	}
	applyIdemSet(row, op.UpdateExpression, op.ExpressionAttributeNames, op.ExpressionAttributeValues)
	f.rows[key] = row
	return nil
}

func (f *idemKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("idemKV: UpdateReturning not supported")
}

func (f *idemKV) Delete(_ context.Context, op kv.DeleteOp) error {
	key := op.Key.PK + "\x00" + op.Key.SK
	row, exists := f.rows[key]
	if !exists || !idemConditionOK(row, op.ConditionExpression, nil, op.ExpressionAttributeValues) {
		return kv.ErrConditionFailed
	}
	delete(f.rows, key)
	return nil
}

func (f *idemKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("idemKV: Query not supported")
}

func (f *idemKV) TransactWrite(context.Context, []kv.WriteOp) error {
	return errors.New("idemKV: TransactWrite not supported")
}

func idemRowToMap(item any) (map[string]any, bool) {
	row, ok := item.(idemRow)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"PK":          row.PK,
		"SK":          row.SK,
		"input_hash":  row.InputHash,
		"status":      row.Status,
		"claim_token": row.ClaimToken,
		"lease_until": row.LeaseUntil,
		"attempts":    row.Attempts,
		"ttl_epoch":   row.TTLEpoch,
		"created_at":  row.CreatedAt,
		"updated_at":  row.UpdatedAt,
	}, true
}

func idemConditionOK(row map[string]any, expr string, names kv.Names, vals kv.Values) bool {
	if expr == "" {
		return true
	}
	if strings.Contains(expr, "claim_token = :t") {
		token, _ := vals[":t"].(string)
		if row["claim_token"] != token {
			return false
		}
	}
	if strings.Contains(expr, "#s = :claimed") {
		claimed, _ := vals[":claimed"].(string)
		if row[resolveIdemName("#s", names)] != claimed {
			return false
		}
	}
	if strings.Contains(expr, "attribute_exists(claim_token)") {
		if _, ok := row["claim_token"]; !ok {
			return false
		}
	}
	if strings.Contains(expr, "lease_until <= :now") {
		now, _ := vals[":now"].(int64)
		leaseUntil, _ := row["lease_until"].(int64)
		if leaseUntil > now {
			return false
		}
	}
	return true
}

func applyIdemSet(row map[string]any, expr string, names kv.Names, vals kv.Values) {
	body := strings.TrimPrefix(strings.TrimSpace(expr), "SET ")
	for _, part := range strings.Split(body, ",") {
		lhs, rhs, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		attr := resolveIdemName(strings.TrimSpace(lhs), names)
		rhs = strings.TrimSpace(rhs)
		if strings.Contains(rhs, "+") {
			parts := strings.Fields(rhs)
			if len(parts) == 3 {
				base := readIdemInt(row[parts[0]])
				delta := readIdemInt(vals[parts[2]])
				row[attr] = int(base + delta)
			}
			continue
		}
		if v, ok := vals[rhs]; ok {
			row[attr] = v
		}
	}
}

func resolveIdemName(name string, names kv.Names) string {
	if strings.HasPrefix(name, "#") {
		if resolved, ok := names[name]; ok {
			return resolved
		}
	}
	return name
}

func readIdemInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

func cloneIdemRow(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

var _ kv.KV = (*idemKV)(nil)
