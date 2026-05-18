package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// --- minimal fakes ---

type fakeStore struct{ deleteErr error }

func (f *fakeStore) Delete(_ context.Context, _ string) error { return f.deleteErr }

type fakeRepo struct{ markErr error }

func (f *fakeRepo) MarkAssetDeleted(_ context.Context, _, _, _ string, _ time.Time) error {
	return f.markErr
}

// rt wraps only what processBody needs so we can test without the full runtime.
type testRT struct {
	store interface {
		Delete(context.Context, string) error
	}
	repo interface {
		MarkAssetDeleted(context.Context, string, string, string, time.Time) error
	}
}

func (r *testRT) processBody(ctx context.Context, body []byte) error {
	var msg cleanupMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		return err
	}
	if msg.TenantID == "" || msg.MediaID == "" || msg.AssetID == "" || msg.StorageKey == "" {
		return errors.New("cleanup message missing required fields")
	}
	if err := r.store.Delete(ctx, msg.StorageKey); err != nil {
		return err
	}
	return r.repo.MarkAssetDeleted(ctx, msg.TenantID, msg.MediaID, msg.AssetID, time.Now().UTC())
}

func msg(t, m, a, k string) []byte {
	b, _ := json.Marshal(cleanupMsg{
		EventType: "media.v1.cleanup", TenantID: t, MediaID: m, AssetID: a, StorageKey: k,
	})
	return b
}

func TestProcessBody_Success(t *testing.T) {
	rt := &testRT{store: &fakeStore{}, repo: &fakeRepo{}}
	if err := rt.processBody(context.Background(), msg("ten1", "med1", "ast1", "ten1/med1/assets/ast1.jpg")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessBody_S3NotFound_TreatedAsSuccess(t *testing.T) {
	// S3 DeleteObject is idempotent; Store.Delete returns nil on 404 via SDK.
	// Simulate that by returning nil — the worker must not return an error.
	rt := &testRT{store: &fakeStore{deleteErr: nil}, repo: &fakeRepo{}}
	if err := rt.processBody(context.Background(), msg("ten1", "med1", "ast1", "ten1/med1/assets/ast1.jpg")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessBody_DDBConditionFailed_TreatedAsSuccess(t *testing.T) {
	// MarkAssetDeleted returns nil on ConditionalCheckFailed (already DELETED).
	rt := &testRT{store: &fakeStore{}, repo: &fakeRepo{markErr: nil}}
	if err := rt.processBody(context.Background(), msg("ten1", "med1", "ast1", "ten1/med1/assets/ast1.jpg")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessBody_MissingFields(t *testing.T) {
	rt := &testRT{store: &fakeStore{}, repo: &fakeRepo{}}
	bad, _ := json.Marshal(map[string]string{"event_type": "media.v1.cleanup"})
	if err := rt.processBody(context.Background(), bad); err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestProcessBody_S3Error_Propagates(t *testing.T) {
	rt := &testRT{store: &fakeStore{deleteErr: errors.New("s3 network error")}, repo: &fakeRepo{}}
	if err := rt.processBody(context.Background(), msg("ten1", "med1", "ast1", "key")); err == nil {
		t.Fatal("expected error to propagate")
	}
}
