package derive

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// deleteRepo is a minimal Repository that captures soft-delete-relevant calls.
// Only the methods that handleDelete actually touches need to be functional;
// the rest are typed stubs so the type assertion in handleDelete succeeds.
type deleteRepo struct {
	mu sync.Mutex

	// listAssets is what ListAssets returns for the (tenant, media) under test.
	listAssets []media.Asset

	// markedDeleted records every (tenantID, mediaID, assetID) tuple flipped
	// to DELETED so tests can assert idempotency and ordering.
	markedDeleted []deletedRow
}

type deletedRow struct {
	tenantID, mediaID, assetID string
	at                         time.Time
}

func (r *deleteRepo) ListAssets(_ context.Context, _, _ string) ([]media.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]media.Asset, len(r.listAssets))
	copy(out, r.listAssets)
	return out, nil
}

func (r *deleteRepo) MarkAssetDeleted(_ context.Context, tenantID, mediaID, assetID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Mirror the production conditional `lifecycle <> :deleted` semantics:
	// flip the in-memory row to DELETED so a second call collapses to a no-op
	// just like the DDB-backed implementation. (Asset has no DeletedAt struct
	// field — the DDB row carries deleted_at + ttl_epoch as attributes; the
	// lifecycle flip is the visible-from-Go signal.)
	for i := range r.listAssets {
		if r.listAssets[i].TenantID == tenantID && r.listAssets[i].MediaID == mediaID && r.listAssets[i].ID == assetID {
			if r.listAssets[i].Lifecycle == media.AssetLifecycleDeleted {
				return nil
			}
			r.listAssets[i].Lifecycle = media.AssetLifecycleDeleted
			r.listAssets[i].UpdatedAt = now
		}
	}
	r.markedDeleted = append(r.markedDeleted, deletedRow{tenantID, mediaID, assetID, now})
	return nil
}

// The remaining Repository methods are not exercised by handleDelete but must
// exist to satisfy the interface. They panic so a future caller doesn't get
// silent zero values.

func (r *deleteRepo) PutMedia(context.Context, media.Media) error { panic("not used") }
func (r *deleteRepo) GetMedia(context.Context, string, string) (*media.Media, error) {
	panic("not used")
}
func (r *deleteRepo) PutAsset(context.Context, media.Asset) error { panic("not used") }
func (r *deleteRepo) GetAsset(context.Context, string, string, string) (*media.Asset, error) {
	panic("not used")
}
func (r *deleteRepo) FindByRole(context.Context, string, string, media.AssetRole, mediaapp.FindByRoleOpts) (*media.Asset, error) {
	panic("not used")
}
func (r *deleteRepo) ListByTenant(context.Context, string, mediaapp.ListOpts) (mediaapp.ListPage, error) {
	panic("not used")
}
func (r *deleteRepo) RetryAsset(context.Context, string, string, string, uint32, mediaapp.OutboxRow, time.Time) (*media.Asset, error) {
	panic("not used")
}
func (r *deleteRepo) SoftDeleteMediaAndEnqueue(context.Context, string, string, time.Duration, mediaapp.OutboxRow, time.Time) error {
	panic("not used")
}
func (r *deleteRepo) InitPresignedUpload(context.Context, media.Media, media.Asset, string, string, time.Duration) (media.Media, media.Asset, error) {
	panic("not used")
}
func (r *deleteRepo) CompletePresignedUpload(context.Context, media.Asset, string, string, mediaapp.OutboxRow, string, string, time.Duration, time.Time) error {
	panic("not used")
}
func (r *deleteRepo) FailPresignedUpload(context.Context, string, string, string, mediaapp.OutboxRow, time.Time) error {
	panic("not used")
}
func (r *deleteRepo) PutAssetIfAbsent(context.Context, media.Asset) (bool, error) {
	panic("not used")
}
func (r *deleteRepo) CompleteMediaIfProcessing(context.Context, string, string, time.Time) error {
	panic("not used")
}
func (r *deleteRepo) PutImageMetadata(context.Context, media.ImageMetadata) error {
	panic("not used")
}

// fakeStorage is a Storage port whose Delete is scripted per-key. Calls are
// recorded so we can assert both "every key was attempted" and "the key was
// attempted exactly once on re-delivery".
type fakeStorage struct {
	mu       sync.Mutex
	deleteFn func(key string) error
	calls    []string
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	s.calls = append(s.calls, key)
	fn := s.deleteFn
	s.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(key)
}

// Unused Storage methods satisfy the port without surprising the test.
func (s *fakeStorage) Put(context.Context, storage.PutInput) (storage.PutOutput, error) {
	panic("not used")
}
func (s *fakeStorage) Get(context.Context, string) (io.ReadCloser, error) { panic("not used") }
func (s *fakeStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	panic("not used")
}
func (s *fakeStorage) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	panic("not used")
}
func (s *fakeStorage) GetObjectAttributes(context.Context, string) (storage.ObjectAttrs, error) {
	panic("not used")
}

type processRepo struct {
	mu       sync.Mutex
	media    map[string]media.Media
	assets   map[string]media.Asset
	meta     []media.ImageMetadata
	inserted []media.Asset
}

func newProcessRepo() *processRepo {
	return &processRepo{
		media:  map[string]media.Media{},
		assets: map[string]media.Asset{},
	}
}

func (r *processRepo) PutMedia(_ context.Context, m media.Media) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.media[m.TenantID+"#"+m.ID] = m
	return nil
}

func (r *processRepo) GetMedia(_ context.Context, tenantID, mediaID string) (*media.Media, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.media[tenantID+"#"+mediaID]
	if !ok {
		return nil, errors.New("media not found")
	}
	return &m, nil
}

func (r *processRepo) PutAsset(_ context.Context, a media.Asset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assets[a.TenantID+"#"+a.MediaID+"#"+a.ID] = a
	r.inserted = append(r.inserted, a)
	return nil
}

func (r *processRepo) PutAssetIfAbsent(_ context.Context, a media.Asset) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := a.TenantID + "#" + a.MediaID + "#" + a.ID
	if _, ok := r.assets[key]; ok {
		return false, nil
	}
	r.assets[key] = a
	r.inserted = append(r.inserted, a)
	return true, nil
}

func (r *processRepo) GetAsset(_ context.Context, tenantID, mediaID, assetID string) (*media.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.assets[tenantID+"#"+mediaID+"#"+assetID]
	if !ok {
		return nil, errors.New("asset not found")
	}
	return &a, nil
}

func (r *processRepo) PutImageMetadata(_ context.Context, meta media.ImageMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta = append(r.meta, meta)
	return nil
}

func (r *processRepo) CompleteMediaIfProcessing(_ context.Context, tenantID, mediaID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tenantID + "#" + mediaID
	m := r.media[key]
	if m.Lifecycle == media.LifecycleRunning {
		m.Lifecycle = media.LifecycleComplete
		m.UpdatedAt = now
		r.media[key] = m
	}
	return nil
}

func (r *processRepo) FindByRole(context.Context, string, string, media.AssetRole, mediaapp.FindByRoleOpts) (*media.Asset, error) {
	panic("not used")
}
func (r *processRepo) ListAssets(context.Context, string, string) ([]media.Asset, error) {
	panic("not used")
}
func (r *processRepo) ListByTenant(context.Context, string, mediaapp.ListOpts) (mediaapp.ListPage, error) {
	panic("not used")
}
func (r *processRepo) RetryAsset(context.Context, string, string, string, uint32, mediaapp.OutboxRow, time.Time) (*media.Asset, error) {
	panic("not used")
}
func (r *processRepo) SoftDeleteMediaAndEnqueue(context.Context, string, string, time.Duration, mediaapp.OutboxRow, time.Time) error {
	panic("not used")
}
func (r *processRepo) InitPresignedUpload(context.Context, media.Media, media.Asset, string, string, time.Duration) (media.Media, media.Asset, error) {
	panic("not used")
}
func (r *processRepo) CompletePresignedUpload(context.Context, media.Asset, string, string, mediaapp.OutboxRow, string, string, time.Duration, time.Time) error {
	panic("not used")
}
func (r *processRepo) FailPresignedUpload(context.Context, string, string, string, mediaapp.OutboxRow, time.Time) error {
	panic("not used")
}
func (r *processRepo) MarkAssetDeleted(context.Context, string, string, string, time.Time) error {
	panic("not used")
}

type processStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    map[string]storage.PutInput
}

func newProcessStorage() *processStorage {
	return &processStorage{objects: map[string][]byte{}, puts: map[string]storage.PutInput{}}
}

func (s *processStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *processStorage) Put(_ context.Context, in storage.PutInput) (storage.PutOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(in.Body)
	s.objects[in.Key] = body
	s.puts[in.Key] = in
	return storage.PutOutput{Key: in.Key, SHA256Hex: "sha256", SizeBytes: int64(len(body))}, nil
}

func (s *processStorage) Delete(context.Context, string) error { panic("not used") }
func (s *processStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	panic("not used")
}
func (s *processStorage) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	panic("not used")
}
func (s *processStorage) GetObjectAttributes(context.Context, string) (storage.ObjectAttrs, error) {
	panic("not used")
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 64, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return out.Bytes()
}

func newAssets() []media.Asset {
	return []media.Asset{
		{ID: "a1", MediaID: "m1", TenantID: "t1", Lifecycle: media.AssetLifecycleComplete, StorageKey: "t1/m1/assets/a1.bin"},
		{ID: "a2", MediaID: "m1", TenantID: "t1", Lifecycle: media.AssetLifecycleComplete, StorageKey: "t1/m1/assets/a2.bin"},
		{ID: "a3", MediaID: "m1", TenantID: "t1", Lifecycle: media.AssetLifecycleFailed, StorageKey: "t1/m1/assets/a3.bin"},
	}
}

func TestHandleProcess_ImageWritesMetadataAndThumbnail(t *testing.T) {
	repo := newProcessRepo()
	store := newProcessStorage()
	now := time.Unix(1700000000, 0).UTC()
	mediaRow := media.Media{
		ID:              "m1",
		TenantID:        "t1",
		Type:            media.TypeImage,
		Lifecycle:       media.LifecycleRunning,
		OriginalAssetID: "orig",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	orig := media.Asset{
		ID:          "orig",
		MediaID:     "m1",
		TenantID:    "t1",
		Kind:        media.AssetKindOriginal,
		Role:        media.AssetRoleOriginal,
		Lifecycle:   media.AssetLifecycleComplete,
		StorageKey:  "t1/m1/assets/orig.png",
		ContentType: "image/png",
		Extension:   "png",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	repo.media["t1#m1"] = mediaRow
	repo.assets["t1#m1#orig"] = orig
	store.objects[orig.StorageKey] = pngBytes(t, 640, 320)
	h := &Handler{Repo: repo, Storage: store, Now: func() time.Time { return now }}

	err := h.HandleEvent(context.Background(), events.MediaEvent{
		MessageID: "evt-1", EventType: events.EventMediaProcess, TenantID: "t1", MediaID: "m1",
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(repo.meta) != 1 {
		t.Fatalf("metadata rows = %d, want 1", len(repo.meta))
	}
	if repo.meta[0].Width != 640 || repo.meta[0].Height != 320 || repo.meta[0].Format != "PNG" {
		t.Fatalf("metadata = %+v, want 640x320 PNG", repo.meta[0])
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("inserted assets = %d, want thumbnail", len(repo.inserted))
	}
	thumb := repo.inserted[0]
	if thumb.Role != media.AssetRoleThumbnail || thumb.Operation != media.AssetOperationImageThumbnail || thumb.ContentType != "image/png" {
		t.Fatalf("thumbnail asset = %+v", thumb)
	}
	if _, ok := store.objects[thumb.StorageKey]; !ok {
		t.Fatalf("thumbnail bytes not stored at %q", thumb.StorageKey)
	}
	if repo.media["t1#m1"].Lifecycle != media.LifecycleComplete {
		t.Fatalf("media lifecycle = %s, want COMPLETE", repo.media["t1#m1"].Lifecycle)
	}
}

// TestHandleDelete_FlipsAllAssetsAfterS3Delete locks the symmetric contract:
// after the worker processes media.v1.delete the entire asset partition is
// tombstoned (lifecycle=DELETED), matching the parent Media row's shape.
func TestHandleDelete_FlipsAllAssetsAfterS3Delete(t *testing.T) {
	repo := &deleteRepo{listAssets: newAssets()}
	store := &fakeStorage{}
	h := &Handler{Repo: repo, Storage: store, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}

	err := h.HandleEvent(context.Background(), events.MediaEvent{
		EventType: events.EventMediaDelete, TenantID: "t1", MediaID: "m1",
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(store.calls) != 3 {
		t.Fatalf("expected 3 S3 deletes, got %d", len(store.calls))
	}
	for _, a := range repo.listAssets {
		if a.Lifecycle != media.AssetLifecycleDeleted {
			t.Fatalf("asset %s lifecycle = %s, want DELETED", a.ID, a.Lifecycle)
		}
	}
	if len(repo.markedDeleted) != 3 {
		t.Fatalf("expected 3 MarkAssetDeleted calls, got %d", len(repo.markedDeleted))
	}
}

// TestHandleDelete_IsIdempotentOnReDelivery is the at-least-once guard. A
// second pass over the same media must leave rows DELETED and must not
// re-issue S3 deletes for assets the first pass already flipped — both come
// from the up-front "skip already-DELETED" check inside handleDelete.
func TestHandleDelete_IsIdempotentOnReDelivery(t *testing.T) {
	repo := &deleteRepo{listAssets: newAssets()}
	store := &fakeStorage{}
	h := &Handler{Repo: repo, Storage: store, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}

	evt := events.MediaEvent{EventType: events.EventMediaDelete, TenantID: "t1", MediaID: "m1"}
	if err := h.HandleEvent(context.Background(), evt); err != nil {
		t.Fatalf("first HandleEvent: %v", err)
	}
	firstCalls := len(store.calls)
	if err := h.HandleEvent(context.Background(), evt); err != nil {
		t.Fatalf("second HandleEvent: %v", err)
	}
	if len(store.calls) != firstCalls {
		t.Fatalf("re-delivery issued extra S3 deletes: first=%d second=%d", firstCalls, len(store.calls))
	}
	for _, a := range repo.listAssets {
		if a.Lifecycle != media.AssetLifecycleDeleted {
			t.Fatalf("asset %s lifecycle = %s, want DELETED after re-delivery", a.ID, a.Lifecycle)
		}
	}
}

// TestHandleDelete_TransientS3FailureLeavesAssetUnflipped locks the
// failed-state-first ordering: if S3 returns a transient error the row stays
// in its prior lifecycle so SQS redelivers and the next pass converges. A
// premature tombstone here would orphan bytes that still exist in S3.
func TestHandleDelete_TransientS3FailureLeavesAssetUnflipped(t *testing.T) {
	repo := &deleteRepo{listAssets: newAssets()}
	transient := errors.New("s3: connection reset")
	store := &fakeStorage{deleteFn: func(key string) error {
		if key == "t1/m1/assets/a2.bin" {
			return transient
		}
		return nil
	}}
	h := &Handler{Repo: repo, Storage: store, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}

	err := h.HandleEvent(context.Background(), events.MediaEvent{
		EventType: events.EventMediaDelete, TenantID: "t1", MediaID: "m1",
	})
	if err == nil {
		t.Fatal("expected S3 error to propagate so SQS redelivers")
	}
	if !errors.Is(err, transient) {
		t.Fatalf("error chain missing transient cause: %v", err)
	}
	// a1 was processed before a2 failed → flipped.
	if repo.listAssets[0].Lifecycle != media.AssetLifecycleDeleted {
		t.Fatalf("a1 should be DELETED before the failure: lifecycle=%s", repo.listAssets[0].Lifecycle)
	}
	// a2 failed at S3 → must NOT be DELETED so the next redelivery can retry.
	if repo.listAssets[1].Lifecycle == media.AssetLifecycleDeleted {
		t.Fatal("a2 was tombstoned despite S3 delete failure")
	}
	// a3 never got processed.
	if repo.listAssets[2].Lifecycle == media.AssetLifecycleDeleted {
		t.Fatal("a3 was tombstoned despite the loop aborting earlier")
	}
}
