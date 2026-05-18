package persist_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// fakeKV is the minimum kv.KV surface persist exercises: Put + Get, with
// the same MarshalMap/UnmarshalMap round-trip the real DynamoDB driver
// does, so the test catches dynamodbav-tag drift the same way prod would.
type fakeKV struct {
	rows map[string]map[string]any
}

func newFakeKV() *fakeKV { return &fakeKV{rows: map[string]map[string]any{}} }

func (f *fakeKV) Put(_ context.Context, item kv.Item, _ kv.PutOptions) error {
	row, ok := item.(map[string]any)
	if !ok {
		return errors.New("fakeKV: only map items supported")
	}
	pk, _ := row["PK"].(string)
	sk, _ := row["SK"].(string)
	clone := make(map[string]any, len(row))
	for k, v := range row {
		clone[k] = v
	}
	f.rows[pk+"\x00"+sk] = clone
	return nil
}

func (f *fakeKV) Get(_ context.Context, key kv.Key, out any) error {
	row, ok := f.rows[key.PK+"\x00"+key.SK]
	if !ok {
		return kv.ErrNotFound
	}
	av, err := attributevalue.MarshalMap(row)
	if err != nil {
		return err
	}
	return attributevalue.UnmarshalMap(av, out)
}

func (f *fakeKV) Update(context.Context, kv.UpdateOp) error {
	return errors.New("fakeKV: Update unsupported")
}
func (f *fakeKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("fakeKV: UpdateReturning unsupported")
}
func (f *fakeKV) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("fakeKV: Delete unsupported")
}
func (f *fakeKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("fakeKV: Query unsupported")
}
func (f *fakeKV) TransactWrite(context.Context, []kv.WriteOp) error {
	return errors.New("fakeKV: TransactWrite unsupported")
}

func TestNewCompletedClaim_Defaults(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	row := persist.NewCompletedClaim("UPLOAD_INIT#t#k", "hash", "m1/a1", now, time.Hour)
	if row["PK"] != "IDEMPOTENCY#UPLOAD_INIT#t#k" {
		t.Fatalf("PK = %v, want IDEMPOTENCY#UPLOAD_INIT#t#k", row["PK"])
	}
	if row["SK"] != persist.ClaimSK {
		t.Fatalf("SK = %v, want %v", row["SK"], persist.ClaimSK)
	}
	if row["status"] != string(idempotency.StatusCompleted) {
		t.Fatalf("status = %v, want COMPLETED", row["status"])
	}
	if _, ok := row["tenant_id"]; ok {
		t.Fatalf("tenant_id present without WithMetadata: %v", row["tenant_id"])
	}
}

func TestWithMetadata_StampsTypedFields(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	row := persist.NewCompletedClaim("UPLOAD_INIT#t#k", "hash", "m1/a1", now, time.Hour,
		persist.WithMetadata(map[string]string{
			"tenant_id": "t1",
			"media_id":  "m1",
			"asset_id":  "a1",
		}),
	)
	if row["tenant_id"] != "t1" || row["media_id"] != "m1" || row["asset_id"] != "a1" {
		t.Fatalf("metadata fields not stamped: tenant_id=%v media_id=%v asset_id=%v",
			row["tenant_id"], row["media_id"], row["asset_id"])
	}
}

// TestWithMetadata_RejectsReservedKeys ensures a caller cannot displace
// claim bookkeeping (status, result, ttl, etc.) by passing a metadata key
// with the same name.
func TestWithMetadata_RejectsReservedKeys(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	row := persist.NewCompletedClaim("UPLOAD_INIT#t#k", "hash", "m1/a1", now, time.Hour,
		persist.WithMetadata(map[string]string{
			"result":  "POISONED",
			"status":  "POISONED",
			"PK":      "POISONED",
			"tenant_id": "t1",
		}),
	)
	if row["result"] != "m1/a1" {
		t.Fatalf("metadata leaked into result: %v", row["result"])
	}
	if row["status"] != string(idempotency.StatusCompleted) {
		t.Fatalf("metadata leaked into status: %v", row["status"])
	}
	if row["PK"] != "IDEMPOTENCY#UPLOAD_INIT#t#k" {
		t.Fatalf("metadata leaked into PK: %v", row["PK"])
	}
	if row["tenant_id"] != "t1" {
		t.Fatalf("legitimate metadata dropped: %v", row["tenant_id"])
	}
}

func TestGetResultWithHashAndMetadata_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newFakeKV()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	row := persist.NewCompletedClaim("UPLOAD_INIT#t#k", "hash-1", "med-1/asset-1", now, time.Hour,
		persist.WithMetadata(map[string]string{
			"tenant_id": "t1",
			"media_id":  "med-1",
			"asset_id":  "asset-1",
		}),
	)
	if err := store.Put(ctx, row, kv.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}

	result, hash, status, metadata, err := persist.GetResultWithHashAndMetadata(ctx, store, "UPLOAD_INIT#t#k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if result != "med-1/asset-1" {
		t.Fatalf("result = %q, want med-1/asset-1", result)
	}
	if hash != "hash-1" {
		t.Fatalf("hash = %q, want hash-1", hash)
	}
	if status != idempotency.StatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", status)
	}
	want := map[string]string{"tenant_id": "t1", "media_id": "med-1", "asset_id": "asset-1"}
	if len(metadata) != len(want) {
		t.Fatalf("metadata size = %d, want %d (metadata=%v)", len(metadata), len(want), metadata)
	}
	for k, v := range want {
		if metadata[k] != v {
			t.Fatalf("metadata[%q] = %q, want %q", k, metadata[k], v)
		}
	}
}

func TestGetResultWithHashAndMetadata_StripsReservedFields(t *testing.T) {
	ctx := context.Background()
	store := newFakeKV()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	row := persist.NewCompletedClaim("scope", "hash", "result-payload", now, time.Hour)
	if err := store.Put(ctx, row, kv.PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, _, _, metadata, err := persist.GetResultWithHashAndMetadata(ctx, store, "scope")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, reserved := range []string{"PK", "SK", "input_hash", "status", "result", "claim_token", "created_at", "updated_at"} {
		if _, ok := metadata[reserved]; ok {
			t.Fatalf("reserved field %q leaked into metadata: %v", reserved, metadata[reserved])
		}
	}
}

func TestGetResultWithHashAndMetadata_NotFoundPropagates(t *testing.T) {
	ctx := context.Background()
	store := newFakeKV()
	_, _, _, _, err := persist.GetResultWithHashAndMetadata(ctx, store, "missing")
	if !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("err = %v, want kv.ErrNotFound", err)
	}
}
