package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

type IdempotencyAdmin struct {
	KV       kv.KV
	Recorder auditapp.Recorder
	Now      func() time.Time
}

type IdempotencyClaim struct {
	Scope           string
	Status          string
	InputHash       string
	ResultRef       string
	ClaimedAt       time.Time
	CompletedAt     time.Time
	TTLAt           time.Time
	Attempts        int32
	LastError       string
	ResetGeneration int32
}

func NewIdempotencyAdmin(k kv.KV, recorder auditapp.Recorder) *IdempotencyAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &IdempotencyAdmin{KV: k, Recorder: recorder, Now: func() time.Time { return time.Now().UTC() }}
}

func (a *IdempotencyAdmin) Inspect(ctx context.Context, scope string) (IdempotencyClaim, error) {
	if scope == "" {
		return IdempotencyClaim{}, errors.New("idempotency admin: scope required")
	}
	var row map[string]any
	if err := a.KV.Get(ctx, persist.Key(scope), &row); err != nil {
		return IdempotencyClaim{}, err
	}
	return decodeIdempotencyClaim(scope, row), nil
}

func (a *IdempotencyAdmin) Reset(ctx context.Context, scope, reason, tenantID, actorUserID string) (int32, error) {
	if scope == "" {
		return 0, errors.New("idempotency admin: scope required")
	}
	if reason == "" {
		return 0, errors.New("idempotency admin: reason required")
	}
	var row map[string]any
	if err := a.KV.Get(ctx, persist.Key(scope), &row); err != nil {
		return 0, err
	}
	oldGen := int32Value(row["reset_generation"])
	newGen := oldGen + 1
	now := a.Now().UTC()
	ev := auditapp.NewIdempotencyClaimReset(actorUserID, scope, int(oldGen), int(newGen), reason, "")
	ev.TenantID = tenantID
	ev.ID = randid.New()
	ev.CreatedAt = now
	err := a.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                auditapp.BuildEventRow(ev),
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Delete: &kv.DeleteOp{
			Key:                 persist.Key(scope),
			ConditionExpression: "attribute_exists(PK) AND attribute_exists(SK)",
		}},
	})
	if err == nil {
		return newGen, nil
	}
	return 0, err
}

func decodeIdempotencyClaim(scope string, row map[string]any) IdempotencyClaim {
	out := IdempotencyClaim{
		Scope:           scope,
		Status:          stringValue(row["status"]),
		InputHash:       stringValue(row["input_hash"]),
		ResultRef:       stringValue(row["result"]),
		Attempts:        int32Value(row["attempts"]),
		LastError:       firstString(row["error_code"], row["last_error"]),
		ResetGeneration: int32Value(row["reset_generation"]),
	}
	out.ClaimedAt = firstTime(row["claimed_at"], row["created_at"])
	out.CompletedAt = firstTime(row["completed_at"])
	if ttl := int64Value(row["ttl_epoch"]); ttl > 0 {
		out.TTLAt = time.Unix(ttl, 0).UTC()
	}
	return out
}

func firstTime(values ...any) time.Time {
	for _, value := range values {
		switch v := value.(type) {
		case time.Time:
			if !v.IsZero() {
				return v.UTC()
			}
		case string:
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil && !t.IsZero() {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return firstString(v)
}

func int32Value(v any) int32 {
	return int32(int64Value(v))
}

func int64Value(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}
