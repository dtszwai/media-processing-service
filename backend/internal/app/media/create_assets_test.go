package media_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

func seedImageMedia(t *testing.T, repo *memRepo, tenantID, mediaID string, lifecycle media.Lifecycle) {
	t.Helper()
	now := time.Now().UTC()
	if err := repo.PutMedia(context.Background(), media.Media{
		ID:        mediaID,
		TenantID:  tenantID,
		Type:      media.TypeImage,
		Lifecycle: lifecycle,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("PutMedia: %v", err)
	}
}

// TestCreateAssets_ImageThumbnail: a fresh request stages a media.v1.process
// outbox row, returns 1 PROCESSING asset ref (thumbnail), and reports
// replay=false. Asset ids are stable across re-runs of the same input.
func TestCreateAssets_ImageThumbnail(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, nil)
	svc.Derive = repo
	seedImageMedia(t, repo, "t1", "med-image", media.LifecycleComplete)

	out, err := svc.CreateAssets(context.Background(), mediaapp.CreateAssetsInput{
		TenantID:       "t1",
		MediaID:        "med-image",
		IdempotencyKey: "client-uuid-1",
		Operations:     []string{"thumbnail"},
	})
	if err != nil {
		t.Fatalf("CreateAssets: %v", err)
	}
	if out.Replay {
		t.Fatalf("expected replay=false on first call")
	}
	if len(out.Assets) != 1 || out.Assets[0].Operation != "thumbnail" {
		t.Fatalf("assets = %+v, want one thumbnail ref", out.Assets)
	}
	if !strings.HasPrefix(out.Assets[0].AssetID, "ast_") {
		t.Fatalf("asset id %q missing ast_ prefix", out.Assets[0].AssetID)
	}
	if out.Assets[0].Lifecycle != string(media.AssetLifecycleProcessing) {
		t.Fatalf("lifecycle = %q, want PROCESSING", out.Assets[0].Lifecycle)
	}
	if len(repo.outbox) != 1 || repo.outbox[0].EventType != "media.v1.process" {
		t.Fatalf("outbox = %+v, want one media.v1.process row", repo.outbox)
	}
}

// TestCreateAssets_ReplaySameInput: second call with the same idempotency
// key and the same input returns the cached asset ids and reports replay=true.
// No second outbox row is staged.
func TestCreateAssets_ReplaySameInput(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, nil)
	svc.Derive = repo
	seedImageMedia(t, repo, "t1", "med-image", media.LifecycleComplete)

	in := mediaapp.CreateAssetsInput{
		TenantID:       "t1",
		MediaID:        "med-image",
		IdempotencyKey: "client-uuid-1",
		Operations:     []string{"thumbnail"},
	}
	first, err := svc.CreateAssets(context.Background(), in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.CreateAssets(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !second.Replay {
		t.Fatalf("expected replay=true on second call with same input")
	}
	if !sameMap(asMap(first.Assets), asMap(second.Assets)) {
		t.Fatalf("asset ids changed across replay: first=%v second=%v", first.Assets, second.Assets)
	}
	if len(repo.outbox) != 1 {
		t.Fatalf("outbox len = %d after replay, want 1 (no second row staged)", len(repo.outbox))
	}
}

// TestCreateAssets_RejectsBadInputs covers the validation surface in one
// place so each rejection has a named test row.
func TestCreateAssets_RejectsBadInputs(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, nil)
	seedImageMedia(t, repo, "t1", "med-image", media.LifecycleComplete)
	seedImageMedia(t, repo, "t1", "med-deleted", media.LifecycleDeleted)
	seedImageMedia(t, repo, "t1", "med-pending", media.LifecyclePending)

	cases := []struct {
		name    string
		in      mediaapp.CreateAssetsInput
		wantErr string
	}{
		{
			name:    "empty operations",
			in:      mediaapp.CreateAssetsInput{TenantID: "t1", MediaID: "med-image", IdempotencyKey: "k"},
			wantErr: "operations required",
		},
		{
			name: "duplicate operations",
			in: mediaapp.CreateAssetsInput{
				TenantID: "t1", MediaID: "med-image", IdempotencyKey: "k",
				Operations: []string{"thumbnail", "thumbnail"},
			},
			wantErr: "duplicate",
		},
		{
			name: "unknown operation",
			in: mediaapp.CreateAssetsInput{
				TenantID: "t1", MediaID: "med-image", IdempotencyKey: "k",
				Operations: []string{"waveform"},
			},
			wantErr: "unknown operation",
		},
		{
			name: "missing idempotency_key",
			in: mediaapp.CreateAssetsInput{
				TenantID: "t1", MediaID: "med-image",
				Operations: []string{"thumbnail"},
			},
			wantErr: "idempotency_key required",
		},
		{
			name: "soft-deleted media",
			in: mediaapp.CreateAssetsInput{
				TenantID: "t1", MediaID: "med-deleted", IdempotencyKey: "k",
				Operations: []string{"thumbnail"},
			},
			wantErr: "soft-deleted",
		},
		{
			name: "pending-upload media",
			in: mediaapp.CreateAssetsInput{
				TenantID: "t1", MediaID: "med-pending", IdempotencyKey: "k",
				Operations: []string{"thumbnail"},
			},
			wantErr: "upload not yet completed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateAssets(context.Background(), tc.in)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestCreateAssets_OutboxBodyShape spot-checks the staged event body so the
// derive worker sees a well-formed MediaEvent. Skipping this would let a
// silent schema drift ship.
func TestCreateAssets_OutboxBodyShape(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, nil)
	svc.Derive = repo
	seedImageMedia(t, repo, "t1", "med-image", media.LifecycleComplete)

	if _, err := svc.CreateAssets(context.Background(), mediaapp.CreateAssetsInput{
		TenantID:       "t1",
		MediaID:        "med-image",
		IdempotencyKey: "client-uuid-1",
		Operations:     []string{"thumbnail"},
	}); err != nil {
		t.Fatalf("CreateAssets: %v", err)
	}
	if len(repo.outbox) != 1 {
		t.Fatalf("outbox len = %d", len(repo.outbox))
	}
	var got struct {
		EventType string `json:"event_type"`
		TenantID  string `json:"tenant_id"`
		MediaID   string `json:"media_id"`
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(repo.outbox[0].Body, &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.EventType != "media.v1.process" {
		t.Fatalf("event_type = %q", got.EventType)
	}
	if got.TenantID != "t1" || got.MediaID != "med-image" {
		t.Fatalf("tenant/media mismatch: %+v", got)
	}
	if !strings.HasPrefix(got.MessageID, "evt-derive-") {
		t.Fatalf("message_id = %q, want evt-derive-…", got.MessageID)
	}
}

// asMap turns []AssetRef into map[op]assetID for stable comparison.
func asMap(refs []mediaapp.AssetRef) map[string]string {
	out := make(map[string]string, len(refs))
	for _, r := range refs {
		out[r.Operation] = r.AssetID
	}
	return out
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
