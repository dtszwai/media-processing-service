package media_test

import (
	"context"
	"errors"
	"testing"
	"time"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

// seedPendingUpload mimics the post-Init state both completion paths land in:
// PENDING media + PENDING_UPLOAD asset rows with a known storage_key. The bytes are
// pre-staged in memStorage so the API path's GetObjectAttributes returns them.
func seedPendingUpload(t *testing.T, repo *memRepo, store *memStorage, key string, body []byte) (mediaID, assetID string) {
	t.Helper()
	now := time.Now().UTC()
	mediaID = "m-event-1"
	assetID = "a-event-1"
	if err := repo.PutMedia(context.Background(), media.Media{
		ID:              mediaID,
		TenantID:        "t",
		Lifecycle:       media.LifecyclePending,
		OriginalAssetID: assetID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("seed media: %v", err)
	}
	if err := repo.PutAsset(context.Background(), media.Asset{
		ID:          assetID,
		MediaID:     mediaID,
		TenantID:    "t",
		Lifecycle:   media.AssetLifecyclePendingUpload,
		StorageKey:  key,
		ContentType: "image/png",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	store.mu.Lock()
	store.objects[key] = body
	store.contentTypes[key] = "image/png"
	store.sha256s[key] = "feedface"
	store.versions[key] = "ver-1"
	store.mu.Unlock()
	return mediaID, assetID
}

// TestCompleteUploadFromS3_HappyPath drives the S3-event ingest path end to
// end: a PENDING row flips to RUNNING, the asset takes the metadata
// the S3 event carried, and the media.v1.process outbox row lands.
func TestCompleteUploadFromS3_HappyPath(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	key := "t/m-event-1/assets/a-event-1.png"
	mediaID, assetID := seedPendingUpload(t, repo, store, key, []byte("png-bytes"))

	out, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: "ver-1",
		SizeBytes:        9,
		SHA256Hex:        "cafebabe",
		ContentType:      "image/png",
		ETag:             "etag-" + key,
	})
	if err != nil {
		t.Fatalf("CompleteUploadFromS3: %v", err)
	}
	if out.Lifecycle != string(media.LifecycleRunning) {
		t.Fatalf("lifecycle = %q, want RUNNING", out.Lifecycle)
	}
	a, _ := repo.GetAsset(context.Background(), "t", mediaID, assetID)
	if a.SHA256 != "feedface" {
		t.Fatalf("sha256 = %q, want feedface", a.SHA256)
	}
	if a.ETag != "etag-"+key {
		t.Fatalf("etag = %q, want %q", a.ETag, "etag-"+key)
	}
	if a.SizeBytes != 9 {
		t.Fatalf("size = %d, want 9", a.SizeBytes)
	}
	if len(repo.outbox) != 1 {
		t.Fatalf("expected 1 outbox row, got %d", len(repo.outbox))
	}
	if len(repo.completeClaims) != 1 {
		t.Fatalf("expected 1 completion claim, got %d", len(repo.completeClaims))
	}
}

// TestCompletionPaths_ConvergeOnOneClaim asserts the property the whole
// refactor exists to deliver: API and S3-event paths race-free against one
// claim scope. The second arrival becomes a no-op (asset stays COMPLETE, no
// second outbox row).
func TestCompletionPaths_ConvergeOnOneClaim(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	mediaID, assetID := seedPendingUpload(t, repo, store, "t/m-event-1/assets/a-event-1.png", []byte("png-bytes"))

	// First completion via S3 event.
	if _, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       "t/m-event-1/assets/a-event-1.png",
		StorageVersionID: "ver-1",
		SizeBytes:        9,
		ETag:             "etag-t/m-event-1/assets/a-event-1.png",
	}); err != nil {
		t.Fatalf("first CompleteUploadFromS3: %v", err)
	}

	// API path arrives second. Lifecycle is already RUNNING; we expect a
	// replay shape, not an error, and the outbox must not have grown.
	beforeOutbox := len(repo.outbox)
	out, err := svc.CompletePresignedUpload(context.Background(), mediaapp.CompleteInput{TenantID: "t", MediaID: mediaID})
	if err != nil {
		t.Fatalf("CompletePresignedUpload (replay): %v", err)
	}
	if out.Lifecycle != string(media.LifecycleRunning) {
		t.Fatalf("replay lifecycle = %q, want RUNNING", out.Lifecycle)
	}
	if len(repo.outbox) != beforeOutbox {
		t.Fatalf("outbox grew on replay: before=%d after=%d", beforeOutbox, len(repo.outbox))
	}
	if len(repo.completeClaims) != 1 {
		t.Fatalf("expected 1 completion claim after both arrivals, got %d", len(repo.completeClaims))
	}
}

func TestCompletionClaimHashIgnoresPathSpecificChecksum(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	key := "t/m-event-1/assets/a-event-1.png"
	mediaID, assetID := seedPendingUpload(t, repo, store, key, []byte("png-bytes"))

	if _, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: "ver-1",
		SizeBytes:        9,
		ETag:             "etag-t/m-event-1/assets/a-event-1.png",
	}); err != nil {
		t.Fatalf("first CompleteUploadFromS3: %v", err)
	}

	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:              mediaID,
		TenantID:        "t",
		Lifecycle:       media.LifecyclePending,
		OriginalAssetID: assetID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID:          assetID,
		MediaID:     mediaID,
		TenantID:    "t",
		Lifecycle:   media.AssetLifecyclePendingUpload,
		StorageKey:  key,
		ContentType: "image/png",
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	beforeOutbox := len(repo.outbox)
	if _, err := svc.CompletePresignedUpload(context.Background(), mediaapp.CompleteInput{TenantID: "t", MediaID: mediaID}); err != nil {
		t.Fatalf("stale API completion should replay same claim despite SHA only being visible to HEAD: %v", err)
	}
	if len(repo.outbox) != beforeOutbox {
		t.Fatalf("outbox grew on same-claim replay: before=%d after=%d", beforeOutbox, len(repo.outbox))
	}
}

// TestCompleteUploadFromS3_KeyMismatchRejects ensures a misrouted S3
// notification can't promote an unrelated asset. The asset's persisted
// storage_key must match the event key — otherwise we reject loudly so the
// caller (worker) returns an error and the queue retries / DLQs.
func TestCompleteUploadFromS3_KeyMismatchRejects(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	mediaID, assetID := seedPendingUpload(t, repo, store, "t/m-event-1/assets/a-event-1.png", []byte("png-bytes"))

	_, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       "t/m-event-1/assets/different-key.png",
		StorageVersionID: "ver-1",
		SizeBytes:        9,
	})
	if err == nil {
		t.Fatal("expected key-mismatch error")
	}
}

// TestCompleteUploadFromS3_RejectsOverCap exercises the size-cap path through
// the S3-event entry point. The shared core's cap check fires regardless of
// which path called it.
func TestCompleteUploadFromS3_RejectsOverCap(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	key := "t/m-event-1/assets/a-event-1.png"
	mediaID, assetID := seedPendingUpload(t, repo, store, key, []byte("png-bytes"))
	store.mu.Lock()
	store.sizes[key] = mediaapp.MaxPresignedUpload + 1
	store.mu.Unlock()

	_, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: "ver-1",
		SizeBytes:        mediaapp.MaxPresignedUpload + 1,
	})
	if err == nil {
		t.Fatal("expected size-cap error")
	}
	m, _ := repo.GetMedia(context.Background(), "t", mediaID)
	if m.Lifecycle != media.LifecycleFailed {
		t.Fatalf("media lifecycle = %q, want FAILED", m.Lifecycle)
	}
}

func TestCompleteUploadFromS3_RejectsUnsupportedContentType(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	key := "t/m-event-1/assets/a-event-1.png"
	mediaID, assetID := seedPendingUpload(t, repo, store, key, []byte("bad-img"))
	store.mu.Lock()
	store.contentTypes[key] = "application/x-unsupported"
	store.mu.Unlock()

	_, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: "ver-1",
		SizeBytes:        7,
		ETag:             "etag-" + key,
	})
	if !errors.Is(err, mediaapp.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	m, _ := repo.GetMedia(context.Background(), "t", mediaID)
	if m.Lifecycle != media.LifecycleFailed {
		t.Fatalf("media lifecycle = %q, want FAILED", m.Lifecycle)
	}
	if len(repo.outbox) != 1 || repo.outbox[0].Reason != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("cleanup outbox = %+v, want unsupported media cleanup", repo.outbox)
	}
}

func TestCompleteUploadFromS3_RejectsMissingObjectContentType(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	key := "t/m-event-1/assets/a-event-1.png"
	mediaID, assetID := seedPendingUpload(t, repo, store, key, []byte("png-bytes"))
	store.mu.Lock()
	delete(store.contentTypes, key)
	store.mu.Unlock()

	_, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: "ver-1",
		SizeBytes:        9,
		ETag:             "etag-" + key,
	})
	if !errors.Is(err, mediaapp.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	m, _ := repo.GetMedia(context.Background(), "t", mediaID)
	if m.Lifecycle != media.LifecycleFailed {
		t.Fatalf("media lifecycle = %q, want FAILED", m.Lifecycle)
	}
}

func TestCompleteUploadFromS3_RejectsEventObjectSizeMismatch(t *testing.T) {
	repo := newMemRepo()
	store := newMemStorage()
	svc := mediaapp.NewService(repo, store)

	key := "t/m-event-1/assets/a-event-1.png"
	mediaID, assetID := seedPendingUpload(t, repo, store, key, []byte("png-bytes"))

	_, err := svc.CompleteUploadFromS3(context.Background(), mediaapp.S3CompleteInput{
		TenantID:         "t",
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: "ver-1",
		SizeBytes:        8,
		ETag:             "etag-" + key,
	})
	if !errors.Is(err, mediaapp.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

// TestParseStorageKey covers the canonical and a representative malformed
// shape so the worker's pre-validation behaviour is locked.
func TestParseStorageKey(t *testing.T) {
	t.Run("canonical", func(t *testing.T) {
		tenant, mediaID, assetID, ext, ok := media.ParseStorageKey("ten/med/assets/ast.png")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if tenant != "ten" || mediaID != "med" || assetID != "ast" || ext != "png" {
			t.Fatalf("got %q/%q/%q/%q", tenant, mediaID, assetID, ext)
		}
	})
	t.Run("no extension", func(t *testing.T) {
		_, _, _, ext, ok := media.ParseStorageKey("ten/med/assets/ast")
		if !ok || ext != "" {
			t.Fatalf("expected ok=true ext=\"\", got ok=%v ext=%q", ok, ext)
		}
	})
	t.Run("wrong prefix rejects", func(t *testing.T) {
		if _, _, _, _, ok := media.ParseStorageKey("provider-staging/foo.bin"); ok {
			t.Fatal("expected ok=false for unrelated prefix")
		}
	})
	t.Run("missing assets segment rejects", func(t *testing.T) {
		if _, _, _, _, ok := media.ParseStorageKey("ten/med/originals/ast.png"); ok {
			t.Fatal("expected ok=false")
		}
	})
}

// guard against accidental coupling: ErrIdempotencyKeyReused should be the
// thing CompletePresignedUploadWithClaim returns on a scope collision with a
// different input hash. The memRepo wires this; this test ensures the public
// error symbol stays addressable.
func TestErrIdempotencyKeyReused_IsExported(t *testing.T) {
	if !errors.Is(mediaapp.ErrIdempotencyKeyReused, mediaapp.ErrIdempotencyKeyReused) {
		t.Fatal("ErrIdempotencyKeyReused identity broken")
	}
}
