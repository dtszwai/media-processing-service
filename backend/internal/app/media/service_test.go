package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// memStorage / memRepo satisfy the application-layer ports without touching AWS.

type memStorage struct {
	mu           sync.Mutex
	objects      map[string][]byte
	contentTypes map[string]string
	sizes        map[string]int64
	sha256s      map[string]string // optional per-key SHA256Hex override
	versions     map[string]string // optional per-key VersionID override
}

func newMemStorage() *memStorage {
	return &memStorage{objects: map[string][]byte{}, contentTypes: map[string]string{}, sizes: map[string]int64{}, sha256s: map[string]string{}, versions: map[string]string{}}
}

func (m *memStorage) Put(_ context.Context, in storage.PutInput) (storage.PutOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf, _ := io.ReadAll(in.Body)
	m.objects[in.Key] = buf
	m.contentTypes[in.Key] = in.ContentType
	return storage.PutOutput{Key: in.Key, SHA256Hex: "deadbeef", SizeBytes: int64(len(buf))}, nil
}

func (m *memStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return io.NopCloser(bytes.NewReader(m.objects[key])), nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "memstorage://" + key, nil
}

func (m *memStorage) PresignPut(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	return "memstorage-put://" + key, nil
}

func (m *memStorage) HeadMetadata(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (m *memStorage) GetObjectAttributes(_ context.Context, key string) (storage.ObjectAttrs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return storage.ObjectAttrs{}, errors.New("memstorage: not found")
	}
	attrs := storage.ObjectAttrs{
		SizeBytes: int64(len(b)),
		ETag:      "etag-" + key,
	}
	if size, ok := m.sizes[key]; ok {
		attrs.SizeBytes = size
	}
	if ct, ok := m.contentTypes[key]; ok {
		attrs.ContentType = ct
	}
	if s, ok := m.sha256s[key]; ok {
		attrs.SHA256Hex = s
	}
	if v, ok := m.versions[key]; ok {
		attrs.VersionID = v
	}
	return attrs, nil
}

type memRepo struct {
	mu             sync.Mutex
	media          map[string]media.Media
	assets         map[string]media.Asset
	claims         map[string]initClaim
	deriveClaims   map[string]deriveClaim
	completeClaims map[string]completeClaim
	outbox         []mediaapp.OutboxRow
	lastListOpts   mediaapp.ListOpts
}

type initClaim struct {
	inputHash string
	mediaID   string
	assetID   string
}

type deriveClaim struct {
	inputHash string
	result    string
}

type completeClaim struct {
	inputHash string
	mediaID   string
	assetID   string
}

func newMemRepo() *memRepo {
	return &memRepo{
		media:          map[string]media.Media{},
		assets:         map[string]media.Asset{},
		claims:         map[string]initClaim{},
		deriveClaims:   map[string]deriveClaim{},
		completeClaims: map[string]completeClaim{},
	}
}

func (m *memRepo) PutMedia(_ context.Context, x media.Media) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.media[x.TenantID+"#"+x.ID] = x
	return nil
}

func (m *memRepo) GetMedia(_ context.Context, tenantID, mediaID string) (*media.Media, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	x, ok := m.media[tenantID+"#"+mediaID]
	if !ok {
		return nil, io.EOF
	}
	return &x, nil
}

func (m *memRepo) PutAsset(_ context.Context, a media.Asset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assets[a.TenantID+"#"+a.MediaID+"#"+a.ID] = a
	return nil
}

func (m *memRepo) GetAsset(_ context.Context, tenantID, mediaID, assetID string) (*media.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.assets[tenantID+"#"+mediaID+"#"+assetID]
	if !ok {
		return nil, io.EOF
	}
	return &a, nil
}

func (m *memRepo) ListAssets(_ context.Context, _, _ string) ([]media.Asset, error) {
	return nil, nil
}

// FindByRole mirrors the DDB contract: highest-priority COMPLETE asset wins,
// ORIGINAL is the fallback partition. The priority ordering matches
// mediaapp.RoleGSISK so the in-memory and DDB selectors converge.
func (m *memRepo) FindByRole(_ context.Context, tenantID, mediaID string, role media.AssetRole, opts mediaapp.FindByRoleOpts) (*media.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.findRoleLocked(tenantID, mediaID, role); a != nil {
		return a, nil
	}
	if opts.AcceptFallback && role != media.AssetRoleOriginal {
		if a := m.findRoleLocked(tenantID, mediaID, media.AssetRoleOriginal); a != nil {
			return a, nil
		}
	}
	return nil, mediaapp.ErrNoAssetForRole
}

func (m *memRepo) findRoleLocked(tenantID, mediaID string, role media.AssetRole) *media.Asset {
	var best *media.Asset
	for _, a := range m.assets {
		a := a
		if a.TenantID != tenantID || a.MediaID != mediaID || a.Role != role {
			continue
		}
		if a.Lifecycle != media.AssetLifecycleComplete {
			continue
		}
		if best == nil || rolePriorityForTest(a.Kind) < rolePriorityForTest(best.Kind) {
			best = &a
		}
	}
	return best
}

func rolePriorityForTest(kind media.AssetKind) int {
	switch kind {
	case media.AssetKindOriginal:
		return 0
	case media.AssetKindGenerated:
		return 1
	case media.AssetKindDerived:
		return 3
	}
	return 999
}

func (m *memRepo) SoftDeleteMedia(_ context.Context, tenantID, mediaID string, _ time.Duration, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tenantID + "#" + mediaID
	x, ok := m.media[key]
	if !ok {
		return io.EOF
	}
	if x.Lifecycle != media.LifecycleRunning && x.Lifecycle != media.LifecycleComplete && x.Lifecycle != media.LifecycleDeleted {
		return errors.New("memrepo: cannot soft-delete from current lifecycle")
	}
	x.Lifecycle = media.LifecycleDeleted
	x.DeletedAt = &now
	x.UpdatedAt = now
	m.media[key] = x
	return nil
}

// SoftDeleteMediaAndEnqueue mirrors the production tx: soft-delete + outbox
// stage in one shot.
func (m *memRepo) SoftDeleteMediaAndEnqueue(ctx context.Context, tenantID, mediaID string, retention time.Duration, row mediaapp.OutboxRow, now time.Time) error {
	if err := m.SoftDeleteMedia(ctx, tenantID, mediaID, retention, now); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outbox = append(m.outbox, row)
	return nil
}

func (m *memRepo) InitPresignedUpload(_ context.Context, x media.Media, a media.Asset, scope, inputHash string, _ time.Duration) (media.Media, media.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.claims[scope]; ok {
		if c.inputHash != inputHash {
			return media.Media{}, media.Asset{}, mediaapp.ErrIdempotencyKeyReused
		}
		pm := m.media[x.TenantID+"#"+c.mediaID]
		pa := m.assets[x.TenantID+"#"+c.mediaID+"#"+c.assetID]
		return pm, pa, nil
	}
	m.media[x.TenantID+"#"+x.ID] = x
	m.assets[a.TenantID+"#"+a.MediaID+"#"+a.ID] = a
	m.claims[scope] = initClaim{inputHash: inputHash, mediaID: x.ID, assetID: a.ID}
	return x, a, nil
}

// CompletePresignedUpload mirrors the DDB-side claim semantics: the scope row
// is staked atomically with the lifecycle flip and a re-arrival with a
// matching hash is an idempotent no-op; a re-arrival with a different hash is
// a hard error.
func (m *memRepo) CompletePresignedUpload(_ context.Context, a media.Asset, mediaID, tenantID string, outboxRow mediaapp.OutboxRow, claimScope, claimInputHash string, _ time.Duration, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.completeClaims[claimScope]; ok {
		if c.inputHash != claimInputHash {
			return mediaapp.ErrIdempotencyKeyReused
		}
		return nil
	}
	if err := m.completeUploadLocked(a, mediaID, tenantID, outboxRow); err != nil {
		return err
	}
	m.completeClaims[claimScope] = completeClaim{inputHash: claimInputHash, mediaID: mediaID, assetID: a.ID}
	return nil
}

func (m *memRepo) completeUploadLocked(a media.Asset, mediaID, tenantID string, outboxRow mediaapp.OutboxRow) error {
	key := tenantID + "#" + mediaID
	x, ok := m.media[key]
	if !ok {
		return io.EOF
	}
	x.Lifecycle = media.LifecycleRunning
	m.media[key] = x
	m.assets[a.TenantID+"#"+a.MediaID+"#"+a.ID] = a
	m.outbox = append(m.outbox, outboxRow)
	return nil
}

// PutAssetIfAbsent mirrors the DDB conditional Put — a second arrival with
// the same key collapses to "not inserted" rather than overwriting.
func (m *memRepo) PutAssetIfAbsent(_ context.Context, a media.Asset) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := a.TenantID + "#" + a.MediaID + "#" + a.ID
	if _, ok := m.assets[key]; ok {
		return false, nil
	}
	m.assets[key] = a
	return true, nil
}

// CompleteMediaIfProcessing flips RUNNING → COMPLETE. Already-COMPLETE or
// DELETED rows are no-ops so re-deliveries converge.
func (m *memRepo) CompleteMediaIfProcessing(_ context.Context, tenantID, mediaID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tenantID + "#" + mediaID
	x, ok := m.media[key]
	if !ok {
		return io.EOF
	}
	if x.Lifecycle == media.LifecycleRunning {
		x.Lifecycle = media.LifecycleComplete
		x.UpdatedAt = now
		m.media[key] = x
	}
	return nil
}

// PutImageMetadata is a no-op fake — the production row lives next to the
// asset row but tests at this layer don't assert on it.
func (m *memRepo) PutImageMetadata(context.Context, media.ImageMetadata) error { return nil }

// MarkAssetDeleted flips an asset to DELETED idempotently.
func (m *memRepo) MarkAssetDeleted(_ context.Context, tenantID, mediaID, assetID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tenantID + "#" + mediaID + "#" + assetID
	if a, ok := m.assets[key]; ok {
		a.Lifecycle = media.AssetLifecycleDeleted
		a.UpdatedAt = now
		m.assets[key] = a
	}
	return nil
}

func (m *memRepo) FailPresignedUpload(_ context.Context, tenantID, mediaID, assetID string, cleanup mediaapp.OutboxRow, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if x, ok := m.media[tenantID+"#"+mediaID]; ok {
		x.Lifecycle = media.LifecycleFailed
		m.media[tenantID+"#"+mediaID] = x
	}
	if a, ok := m.assets[tenantID+"#"+mediaID+"#"+assetID]; ok {
		a.Lifecycle = media.AssetLifecycleFailed
		m.assets[tenantID+"#"+mediaID+"#"+assetID] = a
	}
	m.outbox = append(m.outbox, cleanup)
	return nil
}

func (m *memRepo) ListByTenant(_ context.Context, tenantID string, opts mediaapp.ListOpts) (mediaapp.ListPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastListOpts = opts
	all := make([]media.Media, 0)
	for _, x := range m.media {
		if x.TenantID != tenantID {
			continue
		}
		if !opts.IncludeDeleted && x.Lifecycle == media.LifecycleDeleted {
			continue
		}
		if opts.MediaType != "" && string(x.Type) != opts.MediaType {
			continue
		}
		if opts.Origin != "" && string(x.Origin) != opts.Origin {
			continue
		}
		all = append(all, x)
	}
	// No ordering guarantee in memRepo — sufficient for unit tests.
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return mediaapp.ListPage{Items: all, NextCursor: "", HasMore: false}, nil
}

func (m *memRepo) RetryAsset(_ context.Context, tenantID, mediaID, assetID string, maxAttempts uint32, row mediaapp.OutboxRow, _ time.Time) (*media.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tenantID + "#" + mediaID + "#" + assetID
	a, ok := m.assets[key]
	if !ok {
		return nil, io.EOF
	}
	if a.Lifecycle != media.AssetLifecycleFailed {
		return nil, errors.New("memrepo: asset is not in FAILED state (lifecycle=" + string(a.Lifecycle) + ")")
	}
	if a.Attempts >= maxAttempts {
		return nil, mediaapp.ErrRetryExhausted
	}
	a.Lifecycle = media.AssetLifecycleProcessing
	a.Attempts++
	m.assets[key] = a
	m.outbox = append(m.outbox, row)
	return &a, nil
}

// EnqueueDerive mirrors the DDB-side contract: stake the claim row + stage
// the outbox row. On collision compare input hashes and either replay (same
// hash) or return ErrIdempotencyKeyReused (different hash).
func (m *memRepo) EnqueueDerive(_ context.Context, in mediaapp.DeriveEnqueueInput) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.deriveClaims[in.Scope]; ok {
		if c.inputHash != in.InputHash {
			return "", false, mediaapp.ErrIdempotencyKeyReused
		}
		return c.result, true, nil
	}
	m.deriveClaims[in.Scope] = deriveClaim{inputHash: in.InputHash, result: in.Result}
	m.outbox = append(m.outbox, in.Row)
	return in.Result, false, nil
}

type storageQuotaCall struct {
	tenantID string
	mediaID  string
	assetID  string
	bytes    int64
}

type recordingStorageQuota struct {
	mu       sync.Mutex
	failNext int
	calls    []storageQuotaCall
}

func (q *recordingStorageQuota) RecordStorageBytes(_ context.Context, tenantID, mediaID, assetID string, bytes int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, storageQuotaCall{
		tenantID: tenantID,
		mediaID:  mediaID,
		assetID:  assetID,
		bytes:    bytes,
	})
	if q.failNext > 0 {
		q.failNext--
		return errors.New("quota unavailable")
	}
	return nil
}

func (q *recordingStorageQuota) snapshot() []storageQuotaCall {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]storageQuotaCall(nil), q.calls...)
}

func TestSoftDelete_PopulatesTraceparent(t *testing.T) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	tp := sdktrace.NewTracerProvider()
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test.span")
	defer span.End()

	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())

	// Pre-seed a COMPLETE media row so the soft delete is allowed.
	now := time.Now().UTC()
	_ = repo.PutMedia(ctx, media.Media{
		ID: "m1", TenantID: "t", Lifecycle: media.LifecycleComplete, CreatedAt: now, UpdatedAt: now,
	})

	if err := svc.SoftDelete(ctx, "t", "m1"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if len(repo.outbox) != 1 {
		t.Fatalf("expected 1 outbox row, got %d", len(repo.outbox))
	}
	var evt events.MediaEvent
	if err := json.Unmarshal(repo.outbox[0].Body, &evt); err != nil {
		t.Fatalf("decode outbox body: %v", err)
	}
	if evt.Traceparent == "" || !strings.HasPrefix(evt.Traceparent, "00-") {
		t.Fatalf("traceparent shape unexpected: %q", evt.Traceparent)
	}
}

func TestPresignReady_RejectsDeletedMedia(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())

	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m1", TenantID: "t", Lifecycle: media.LifecycleDeleted, CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: "a1", MediaID: "m1", TenantID: "t", Lifecycle: media.AssetLifecycleComplete, StorageKey: "k",
	})
	if _, err := svc.PresignReady(context.Background(), "t", "m1", "a1"); err == nil {
		t.Fatal("expected error for deleted media")
	}
}

func TestGetVisible_EnforcesOwnerPrivateMedia(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:          "m1",
		TenantID:    "t",
		OwnerUserID: "owner",
		Visibility:  media.VisibilityOwnerPrivate,
		Lifecycle:   media.LifecycleComplete,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	if _, err := svc.GetVisible(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "other"}, "m1"); !errors.Is(err, mediaapp.ErrForbidden) {
		t.Fatalf("other user err = %v, want ErrForbidden", err)
	}
	if _, err := svc.GetVisible(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "owner"}, "m1"); err != nil {
		t.Fatalf("owner GetVisible: %v", err)
	}
	if _, err := svc.GetVisible(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "other", Roles: []string{"ADMIN"}}, "m1"); err != nil {
		t.Fatalf("admin GetVisible: %v", err)
	}
}

func TestGetMutable_RequiresOwnerOrWriteGrant(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:          "m1",
		TenantID:    "t",
		OwnerUserID: "owner",
		Visibility:  media.VisibilityTenantShared,
		Lifecycle:   media.LifecycleComplete,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	if _, err := svc.GetVisible(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "other"}, "m1"); err != nil {
		t.Fatalf("tenant-shared read should still be visible: %v", err)
	}
	if _, err := svc.GetMutable(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "other"}, "m1"); !errors.Is(err, mediaapp.ErrForbidden) {
		t.Fatalf("other user mutable err = %v, want ErrForbidden", err)
	}
	if _, err := svc.GetMutable(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "owner"}, "m1"); err != nil {
		t.Fatalf("owner GetMutable: %v", err)
	}
	if _, err := svc.GetMutable(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "other", Scopes: []string{"media:write:tenant"}}, "m1"); err != nil {
		t.Fatalf("write-scope GetMutable: %v", err)
	}
}

func TestListByPrincipal_FiltersOwnerPrivateMedia(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:          "owned",
		TenantID:    "t",
		OwnerUserID: "owner",
		Visibility:  media.VisibilityOwnerPrivate,
		Lifecycle:   media.LifecycleComplete,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:         "shared",
		TenantID:   "t",
		Visibility: media.VisibilityTenantShared,
		Lifecycle:  media.LifecycleComplete,
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	page, err := svc.ListByPrincipal(context.Background(), mediaapp.Principal{TenantID: "t", UserID: "other"}, mediaapp.ListOpts{Limit: 20})
	if err != nil {
		t.Fatalf("ListByPrincipal: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "shared" {
		t.Fatalf("items = %+v, want only shared", page.Items)
	}
}

func TestInitPresignedUpload_AllocatesIDs(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	out, err := svc.InitPresignedUpload(context.Background(), mediaapp.InitInput{
		TenantID:       "t",
		Filename:       "x.png",
		ContentType:    "image/png",
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !strings.HasPrefix(out.MediaID, "med_") || !strings.HasPrefix(out.AssetID, "ast_") {
		t.Fatalf("ids missing prefixes: %+v", out)
	}
	if !strings.HasPrefix(out.UploadURL, "memstorage-put://") {
		t.Fatalf("upload url unexpected: %q", out.UploadURL)
	}
	m, err := repo.GetMedia(context.Background(), "t", out.MediaID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if m.Visibility != media.VisibilityTenantShared {
		t.Fatalf("visibility = %q, want TENANT_SHARED when no owner is set", m.Visibility)
	}
	if m.Type != media.TypeImage {
		t.Fatalf("media type = %q, want IMAGE", m.Type)
	}
}

func TestInitPresignedUpload_RejectsUnsupportedContentType(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		contentType string
	}{
		{name: "unsupported", filename: "x.bin", contentType: "application/x-unsupported"},
		{name: "generic", filename: "x.bin", contentType: "application/octet-stream"},
		{name: "missing", filename: "x.png", contentType: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemRepo()
			svc := mediaapp.NewService(repo, newMemStorage())
			_, err := svc.InitPresignedUpload(context.Background(), mediaapp.InitInput{
				TenantID:       "t",
				Filename:       tc.filename,
				ContentType:    tc.contentType,
				IdempotencyKey: "key-1",
			})
			if !errors.Is(err, mediaapp.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if len(repo.media) != 0 || len(repo.assets) != 0 {
				t.Fatalf("unsupported upload persisted rows: media=%d assets=%d", len(repo.media), len(repo.assets))
			}
		})
	}
}

func TestInitPresignedUpload_UsesCanonicalExtensionFromContentType(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	out, err := svc.InitPresignedUpload(context.Background(), mediaapp.InitInput{
		TenantID:       "t",
		Filename:       "misleading.bin",
		ContentType:    "image/png",
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !strings.HasSuffix(out.StorageKey, ".png") {
		t.Fatalf("storage key = %q, want canonical .png extension", out.StorageKey)
	}
	a, err := repo.GetAsset(context.Background(), "t", out.MediaID, out.AssetID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if a.Extension != "png" || a.ContentType != "image/png" {
		t.Fatalf("asset media shape = extension %q content_type %q, want png/image/png", a.Extension, a.ContentType)
	}
}

func TestInitPresignedUpload_OwnerDefaultsPrivate(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	out, err := svc.InitPresignedUpload(context.Background(), mediaapp.InitInput{
		TenantID:       "t",
		OwnerUserID:    "owner",
		Filename:       "x.png",
		ContentType:    "image/png",
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	m, err := repo.GetMedia(context.Background(), "t", out.MediaID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if m.Visibility != media.VisibilityOwnerPrivate {
		t.Fatalf("visibility = %q, want OWNER_PRIVATE", m.Visibility)
	}
}

func TestInitPresignedUpload_RejectsWebhookURLWithoutHTTPSHost(t *testing.T) {
	cases := []string{
		"http://example.com/hook",
		"ftp://example.com/hook",
		"https:///hook",
		"https:example.com/hook",
	}
	for _, webhookURL := range cases {
		t.Run(webhookURL, func(t *testing.T) {
			repo := newMemRepo()
			svc := mediaapp.NewService(repo, newMemStorage())
			_, err := svc.InitPresignedUpload(context.Background(), mediaapp.InitInput{
				TenantID:       "t",
				WebhookURL:     webhookURL,
				Filename:       "x.png",
				ContentType:    "image/png",
				IdempotencyKey: "key-1",
			})
			if err == nil {
				t.Fatal("expected invalid webhook URL error")
			}
			if !strings.Contains(err.Error(), "invalid url") {
				t.Fatalf("error = %v, want invalid url", err)
			}
			if len(repo.media) != 0 || len(repo.assets) != 0 {
				t.Fatalf("invalid webhook URL persisted rows: media=%d assets=%d", len(repo.media), len(repo.assets))
			}
		})
	}
}

func TestInitPresignedUpload_StoresTrimmedHTTPSWebhookURL(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	out, err := svc.InitPresignedUpload(context.Background(), mediaapp.InitInput{
		TenantID:       "t",
		WebhookURL:     "  https://example.com/hook?tenant=t  ",
		Filename:       "x.png",
		ContentType:    "image/png",
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	m, err := repo.GetMedia(context.Background(), "t", out.MediaID)
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if m.WebhookURL != "https://example.com/hook?tenant=t" {
		t.Fatalf("webhook URL = %q", m.WebhookURL)
	}
}

func TestInitPresignedUpload_ReplaysOriginalAssetForSameIdempotencyKey(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	in := mediaapp.InitInput{
		TenantID:       "t",
		Filename:       "x.png",
		ContentType:    "image/png",
		IdempotencyKey: "key-1",
	}
	first, err := svc.InitPresignedUpload(context.Background(), in)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	second, err := svc.InitPresignedUpload(context.Background(), in)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if second.MediaID != first.MediaID || second.AssetID != first.AssetID || second.StorageKey != first.StorageKey {
		t.Fatalf("replay allocated new target: first=%+v second=%+v", first, second)
	}
}

// TestInitPresignedUpload_RejectsIdempotencyKeyReusedWithDifferentInput locks
// the contract that the shared row reader enforces: same idempotency_key
// scoped to a tenant but different request shape (here, a different filename
// → different content_type → different hash) must surface as a typed error
// rather than silently returning the cached media/asset ids.
func TestInitPresignedUpload_RejectsIdempotencyKeyReusedWithDifferentInput(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	first := mediaapp.InitInput{
		TenantID:       "t",
		Filename:       "x.png",
		ContentType:    "image/png",
		IdempotencyKey: "key-1",
	}
	if _, err := svc.InitPresignedUpload(context.Background(), first); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	second := first
	second.Filename = "y.png"
	_, err := svc.InitPresignedUpload(context.Background(), second)
	if !errors.Is(err, mediaapp.ErrIdempotencyKeyReused) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestCompletePresignedUpload_StoresETagSeparatelyFromSHA256(t *testing.T) {
	repo := newMemRepo()
	storage := newMemStorage()
	svc := mediaapp.NewService(repo, storage)

	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:              "m1",
		TenantID:        "t",
		Lifecycle:       media.LifecyclePending,
		OriginalAssetID: "a1",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID:          "a1",
		MediaID:     "m1",
		TenantID:    "t",
		Lifecycle:   media.AssetLifecyclePendingUpload,
		StorageKey:  "uploads/m1/a1.png",
		ContentType: "image/png",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	storage.objects["uploads/m1/a1.png"] = []byte("some bytes")
	storage.contentTypes["uploads/m1/a1.png"] = "image/png"

	out, err := svc.CompletePresignedUpload(context.Background(), mediaapp.CompleteInput{TenantID: "t", MediaID: "m1"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out.ETag != "etag-uploads/m1/a1.png" {
		t.Fatalf("output etag = %q", out.ETag)
	}
	a, err := repo.GetAsset(context.Background(), "t", "m1", "a1")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if a.ETag != "etag-uploads/m1/a1.png" {
		t.Fatalf("asset etag = %q", a.ETag)
	}
	if a.SHA256 != "" {
		t.Fatalf("sha256 should remain empty for S3 ETag, got %q", a.SHA256)
	}
}

func TestCompletePresignedUpload_ReplaysStorageQuotaAfterPostTransactionFailure(t *testing.T) {
	repo := newMemRepo()
	storage := newMemStorage()
	svc := mediaapp.NewService(repo, storage)
	quotaRecorder := &recordingStorageQuota{failNext: 1}
	svc.Quota = quotaRecorder

	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:              "m-quota",
		TenantID:        "t",
		Lifecycle:       media.LifecyclePending,
		OriginalAssetID: "a-quota",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID:          "a-quota",
		MediaID:     "m-quota",
		TenantID:    "t",
		Lifecycle:   media.AssetLifecyclePendingUpload,
		StorageKey:  "uploads/m-quota/a-quota.png",
		ContentType: "image/png",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	storage.objects["uploads/m-quota/a-quota.png"] = []byte("quota bytes")
	storage.contentTypes["uploads/m-quota/a-quota.png"] = "image/png"

	_, err := svc.CompletePresignedUpload(context.Background(), mediaapp.CompleteInput{TenantID: "t", MediaID: "m-quota"})
	if err == nil {
		t.Fatal("expected quota accounting error after durable completion")
	}
	if !strings.Contains(err.Error(), "record storage quota") {
		t.Fatalf("error = %v, want storage quota context", err)
	}
	m, err := repo.GetMedia(context.Background(), "t", "m-quota")
	if err != nil {
		t.Fatalf("GetMedia: %v", err)
	}
	if m.Lifecycle != media.LifecycleRunning {
		t.Fatalf("media lifecycle after quota failure = %q, want RUNNING", m.Lifecycle)
	}
	if calls := quotaRecorder.snapshot(); len(calls) != 1 {
		t.Fatalf("quota calls after first completion = %d, want 1", len(calls))
	}
	beforeOutbox := len(repo.outbox)

	out, err := svc.CompletePresignedUpload(context.Background(), mediaapp.CompleteInput{TenantID: "t", MediaID: "m-quota"})
	if err != nil {
		t.Fatalf("replay Complete: %v", err)
	}
	if out.Lifecycle != string(media.LifecycleRunning) {
		t.Fatalf("replay lifecycle = %q, want RUNNING", out.Lifecycle)
	}
	if len(repo.outbox) != beforeOutbox {
		t.Fatalf("outbox grew on quota replay: before=%d after=%d", beforeOutbox, len(repo.outbox))
	}
	calls := quotaRecorder.snapshot()
	if len(calls) != 2 {
		t.Fatalf("quota calls after replay = %d, want 2", len(calls))
	}
	if calls[1].tenantID != "t" || calls[1].mediaID != "m-quota" || calls[1].assetID != "a-quota" || calls[1].bytes != int64(len("quota bytes")) {
		t.Fatalf("replay quota call = %+v", calls[1])
	}
}

func TestCompletePresignedUpload_SHA256FromS3(t *testing.T) {
	repo := newMemRepo()
	storage := newMemStorage()
	svc := mediaapp.NewService(repo, storage)

	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID:              "m2",
		TenantID:        "t",
		Lifecycle:       media.LifecyclePending,
		OriginalAssetID: "a2",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID:          "a2",
		MediaID:     "m2",
		TenantID:    "t",
		Lifecycle:   media.AssetLifecyclePendingUpload,
		StorageKey:  "uploads/m2/a2",
		ContentType: "image/png",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	// Inject a SHA256 so the mock returns it — simulates S3 storing the checksum.
	storage.mu.Lock()
	storage.objects["uploads/m2/a2"] = []byte("png bytes")
	storage.contentTypes["uploads/m2/a2"] = "image/png"
	storage.sha256s = map[string]string{"uploads/m2/a2": "deadbeef1234"}
	storage.mu.Unlock()

	out, err := svc.CompletePresignedUpload(context.Background(), mediaapp.CompleteInput{TenantID: "t", MediaID: "m2"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = out
	a, _ := repo.GetAsset(context.Background(), "t", "m2", "a2")
	if a.SHA256 != "deadbeef1234" {
		t.Fatalf("expected sha256 deadbeef1234 on asset, got %q", a.SHA256)
	}
}

// --- ListByTenant tests (Items 4+5) ---

func TestListByTenant_LimitDefault(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	// opts.Limit == 0 → service clamps to 50
	page, err := svc.ListByTenant(context.Background(), "t", mediaapp.ListOpts{})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	_ = page // empty is fine; we only test that no error occurs
}

func TestListByTenant_LimitClamped(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())

	if _, err := svc.ListByTenant(context.Background(), "t", mediaapp.ListOpts{Limit: 0}); err != nil {
		t.Fatalf("Limit=0: %v", err)
	}
	if got := repo.lastListOpts.Limit; got != 50 {
		t.Fatalf("Limit=0 should clamp to 50, repo saw %d", got)
	}

	if _, err := svc.ListByTenant(context.Background(), "t", mediaapp.ListOpts{Limit: 999}); err != nil {
		t.Fatalf("Limit=999: %v", err)
	}
	if got := repo.lastListOpts.Limit; got != 100 {
		t.Fatalf("Limit=999 should clamp to 100, repo saw %d", got)
	}
}

func TestListByTenant_PassThrough(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()

	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m1", TenantID: "t", Type: media.TypeImage,
		Origin: media.OriginUpload, Lifecycle: media.LifecycleComplete,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m2", TenantID: "t", Type: media.TypeAudio,
		Origin: media.OriginUpload, Lifecycle: media.LifecycleComplete,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m3", TenantID: "t", Type: media.TypeImage,
		Origin: media.OriginUpload, Lifecycle: media.LifecycleDeleted,
		CreatedAt: now, UpdatedAt: now,
	})

	// Default: no deleted
	page, err := svc.ListByTenant(context.Background(), "t", mediaapp.ListOpts{})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items (no deleted), got %d", len(page.Items))
	}

	// IncludeDeleted=true
	page, err = svc.ListByTenant(context.Background(), "t", mediaapp.ListOpts{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ListByTenant IncludeDeleted: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("expected 3 items with deleted, got %d", len(page.Items))
	}

	// MediaType filter
	page, err = svc.ListByTenant(context.Background(), "t", mediaapp.ListOpts{MediaType: "IMAGE"})
	if err != nil {
		t.Fatalf("ListByTenant MediaType: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 IMAGE item (non-deleted), got %d", len(page.Items))
	}
	if page.Items[0].ID != "m1" {
		t.Fatalf("expected m1, got %q", page.Items[0].ID)
	}
}

func TestListByTenant_EmptyResultNeverNilItems(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	page, err := svc.ListByTenant(context.Background(), "nobody", mediaapp.ListOpts{})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if page.Items == nil {
		t.Fatal("Items must not be nil for empty result (clients render null poorly)")
	}
}

// --- RetryAsset tests (Item 11) ---

func TestRetryAsset_HappyPath(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()

	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m1", TenantID: "t", Lifecycle: media.LifecycleRunning,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: "a1", MediaID: "m1", TenantID: "t",
		Lifecycle: media.AssetLifecycleFailed, Attempts: 0,
	})

	asset, err := svc.RetryAsset(context.Background(), "t", "m1", "a1")
	if err != nil {
		t.Fatalf("RetryAsset: %v", err)
	}
	if asset.Lifecycle != media.AssetLifecycleProcessing {
		t.Fatalf("expected PROCESSING, got %q", asset.Lifecycle)
	}
	if asset.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", asset.Attempts)
	}
	if len(repo.outbox) == 0 {
		t.Fatal("expected outbox row to be staged")
	}
}

func TestRetryAsset_ExhaustionReturnsErrRetryExhausted(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()

	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m1", TenantID: "t", Lifecycle: media.LifecycleRunning,
		CreatedAt: now, UpdatedAt: now,
	})
	// Seed asset at MaxRetryAttempts-1 so one more retry succeeds.
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: "a1", MediaID: "m1", TenantID: "t",
		Lifecycle: media.AssetLifecycleFailed, Attempts: mediaapp.MaxRetryAttempts - 1,
	})

	// This retry should succeed (MaxRetryAttempts-1 < MaxRetryAttempts).
	asset, err := svc.RetryAsset(context.Background(), "t", "m1", "a1")
	if err != nil {
		t.Fatalf("penultimate retry: %v", err)
	}
	if asset.Lifecycle != media.AssetLifecycleProcessing {
		t.Fatalf("expected PROCESSING after penultimate retry, got %q", asset.Lifecycle)
	}

	// Reset to FAILED to simulate the next failure cycle.
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: "a1", MediaID: "m1", TenantID: "t",
		Lifecycle: media.AssetLifecycleFailed, Attempts: mediaapp.MaxRetryAttempts,
	})

	// Now we're at the limit — should return ErrRetryExhausted.
	_, err = svc.RetryAsset(context.Background(), "t", "m1", "a1")
	if !errors.Is(err, mediaapp.ErrRetryExhausted) {
		t.Fatalf("expected ErrRetryExhausted, got %v", err)
	}
}

func TestRetryAsset_WrongStateError(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()

	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m1", TenantID: "t", Lifecycle: media.LifecycleComplete,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: "a1", MediaID: "m1", TenantID: "t",
		Lifecycle: media.AssetLifecycleComplete, Attempts: 0,
	})

	_, err := svc.RetryAsset(context.Background(), "t", "m1", "a1")
	if err == nil {
		t.Fatal("expected error for COMPLETE asset")
	}
	if errors.Is(err, mediaapp.ErrRetryExhausted) {
		t.Fatalf("expected non-exhaustion error, got ErrRetryExhausted")
	}
	if !strings.Contains(err.Error(), "COMPLETE") {
		t.Fatalf("expected error message to mention state, got: %v", err)
	}
}

func TestRetryAsset_UnknownAssetPropagatesNotFound(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())

	_, err := svc.RetryAsset(context.Background(), "t", "m-unknown", "a-unknown")
	if err == nil {
		t.Fatal("expected error for unknown asset")
	}
}
