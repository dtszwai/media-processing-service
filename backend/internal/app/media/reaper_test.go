package media_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// mapRow satisfies kv.Row over a plain map.
type mapRow map[string]any

func (r mapRow) Get(name string) any     { return r[name] }
func (r mapRow) Unmarshal(out any) error { return errors.New("mapRow.Unmarshal not implemented") }

// fakeKV is a kv.KV fake that drives Query results from a queue and records
// the request shape for assertion.
type fakeKV struct {
	pages       []kv.QueryResult
	calls       int
	lastIndex   string
	lastKeyExpr string
	lastFilter  string
	lastValues  kv.Values
}

func (f *fakeKV) Query(_ context.Context, req kv.QueryRequest) (kv.QueryResult, error) {
	f.calls++
	f.lastIndex = req.Index
	f.lastKeyExpr = req.KeyConditionExpression
	f.lastFilter = req.FilterExpression
	f.lastValues = req.ExpressionAttributeValues
	if len(f.pages) == 0 {
		return kv.QueryResult{}, nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

// Unused kv.KV methods.
func (f *fakeKV) Put(context.Context, kv.Item, kv.PutOptions) error { return nil }
func (f *fakeKV) Get(context.Context, kv.Key, any) error            { return nil }
func (f *fakeKV) Update(context.Context, kv.UpdateOp) error         { return nil }
func (f *fakeKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, nil
}
func (f *fakeKV) Delete(context.Context, kv.DeleteOp) error         { return nil }
func (f *fakeKV) TransactWrite(context.Context, []kv.WriteOp) error { return nil }

// TestReaper_RunQueriesGSILifecycle verifies that Run issues a Query against
// gsi_lifecycle (not a Scan) and flips the returned rows via FailPresignedUpload.
func TestReaper_RunQueriesGSILifecycle(t *testing.T) {
	const (
		tenantID = "ten1"
		mediaID  = "med_abc"
		assetID  = "ast_xyz"
	)
	now := time.Now().UTC()

	repo := newMemRepo()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID: mediaID, TenantID: tenantID,
		Lifecycle: media.LifecyclePending,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: assetID, MediaID: mediaID, TenantID: tenantID,
		Lifecycle:  media.AssetLifecyclePendingUpload,
		StorageKey: "uploads/" + mediaID + "/" + assetID,
	})

	store := &fakeKV{
		pages: []kv.QueryResult{
			{
				Items: []kv.Row{mapRow{
					"tenant_id":         tenantID,
					"id":                mediaID,
					"original_asset_id": assetID,
					"gsi_lifecycle_pk":  "TENANT#" + tenantID + "#LIFECYCLE#PENDING",
					"gsi_lifecycle_sk":  now.Add(-48 * time.Hour).Format(time.RFC3339Nano),
				}},
			},
		},
	}

	r := mediaapp.NewReaper(store, repo)
	r.Now = func() time.Time { return now }

	flipped, err := r.Run(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if flipped != 1 {
		t.Fatalf("expected 1 flipped, got %d", flipped)
	}
	if store.calls != 1 {
		t.Fatalf("expected 1 Query call, got %d", store.calls)
	}
	if store.lastIndex != "gsi_lifecycle" {
		t.Fatalf("expected IndexName=gsi_lifecycle, got %q", store.lastIndex)
	}
	if !strings.Contains(store.lastKeyExpr, "gsi_lifecycle_pk") {
		t.Fatalf("key condition missing gsi_lifecycle_pk: %q", store.lastKeyExpr)
	}

	m, err := repo.GetMedia(context.Background(), tenantID, mediaID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if m.Lifecycle != media.LifecycleFailed {
		t.Fatalf("expected FAILED lifecycle, got %v", m.Lifecycle)
	}
	if len(repo.outbox) != 1 {
		t.Fatalf("expected 1 cleanup outbox row, got %d", len(repo.outbox))
	}
	var cleanup map[string]string
	if err := json.Unmarshal(repo.outbox[0].Body, &cleanup); err != nil {
		t.Fatalf("decode cleanup body: %v", err)
	}
	if cleanup["storage_key"] != "uploads/"+mediaID+"/"+assetID {
		t.Fatalf("storage_key = %q, want asset storage key", cleanup["storage_key"])
	}
}

// TestReaper_RunEmptyResult returns zero flipped with no error when gsi_lifecycle
// returns an empty page.
func TestReaper_RunEmptyResult(t *testing.T) {
	store := &fakeKV{pages: []kv.QueryResult{{}}}
	r := mediaapp.NewReaper(store, newMemRepo())
	flipped, err := r.Run(context.Background(), "tenant-x")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if flipped != 0 {
		t.Fatalf("expected 0 flipped, got %d", flipped)
	}
}

// TestReaper_RunMissingDepsErrors validates the guard clause.
func TestReaper_RunMissingDepsErrors(t *testing.T) {
	r := &mediaapp.Reaper{}
	if _, err := r.Run(context.Background(), "ten"); err == nil {
		t.Fatal("expected error when kv/repo nil")
	}
}
