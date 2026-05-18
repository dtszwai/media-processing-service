// Package media implements the application-layer media use cases: presigned
// upload init/refresh/complete, fetch, list assets, soft delete, presigned
// download. Presigned upload is the canonical path for all file sizes
// (≤ 1 GB); there is no direct-upload alternative.
package media

import (
	"context"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// Storage is the data-plane port — re-exported from infra/storage so the
// service package keeps its handler signatures stable while the actual port
// lives in infra.
type Storage = storage.Storage

// ListOpts configures a ListByTenant query.
type ListOpts struct {
	Limit          int    // capped to 100, defaults to 50 when ≤ 0
	Cursor         string // opaque continuation token; "" = start
	MediaType      string // exact-match filter on media_type, "" = all
	Origin         string // exact-match filter on origin, "" = all
	IncludeDeleted bool   // false = exclude DELETED lifecycle rows
}

// ListPage is the paginated response from ListByTenant.
type ListPage struct {
	Items      []media.Media `json:"items"`
	NextCursor string        `json:"nextCursor"`
	HasMore    bool          `json:"hasMore"`
}

// ErrNoAssetForRole is returned by FindByRole when no COMPLETE asset of the
// requested role exists for the given media. Transport callers map this to a
// not-found response so the contract is honest about missing derivatives
// rather than silently falling through to the original bytes.
var ErrNoAssetForRole = errors.New("media: no asset for role")

var ErrForbidden = errors.New("media: forbidden")

type Principal struct {
	TenantID string
	UserID   string
	APIKeyID string
	Roles    []string
	Scopes   []string
}

// FindByRoleOpts tunes role-selector resolution.
type FindByRoleOpts struct {
	// AcceptFallback enables the role-priority fallback: when no COMPLETE
	// asset of the exact requested role exists, return the lowest-priority
	// asset of the AssetRoleOriginal partition instead. Used by
	// /download-url so the caller can still get bytes for media types that
	// never produce a dedicated DOWNLOAD-role asset. PREVIEW / THUMBNAIL
	// selectors leave this off — silently degrading those would obscure the
	// fact that a derivative is still in progress.
	AcceptFallback bool
}

// Repository persists Media + Asset rows.
type Repository interface {
	PutMedia(ctx context.Context, m media.Media) error
	GetMedia(ctx context.Context, tenantID, mediaID string) (*media.Media, error)
	PutAsset(ctx context.Context, a media.Asset) error
	GetAsset(ctx context.Context, tenantID, mediaID, assetID string) (*media.Asset, error)
	ListAssets(ctx context.Context, tenantID, mediaID string) ([]media.Asset, error)

	// FindByRole returns the highest-priority COMPLETE asset matching the
	// requested role for the given media. The "highest priority" is encoded
	// by RoleGSISK — see rolePriority for the ordering. Returns
	// ErrNoAssetForRole when no match exists (and the fallback partition is
	// also empty when opts.AcceptFallback is set).
	FindByRole(ctx context.Context, tenantID, mediaID string, role media.AssetRole, opts FindByRoleOpts) (*media.Asset, error)

	// ListByTenant returns a page of Media rows for the tenant, newest first
	// (ScanIndexForward=false against gsi_tenant_media). Soft-deleted rows are
	// excluded unless opts.IncludeDeleted is set. Cursor is opaque to callers.
	ListByTenant(ctx context.Context, tenantID string, opts ListOpts) (ListPage, error)

	// RetryAsset is one TransactWrite: an Asset update flipping FAILED →
	// PROCESSING with attempts++ (condition: lifecycle = FAILED AND attempts <
	// maxAttempts), plus a media-process outbox Put. Returns the updated asset
	// on success; ErrConditionFailed when the asset isn't FAILED or attempts
	// are exhausted.
	RetryAsset(ctx context.Context, tenantID, mediaID, assetID string, maxAttempts uint32, row OutboxRow, now time.Time) (*media.Asset, error)

	// SoftDeleteMediaAndEnqueue flips Lifecycle to DELETED + sets DeletedAt +
	// ExpiresAt (now + retention) and stages a media.v1.delete outbox row in
	// one transaction. Idempotent on already-DELETED rows. Rejects
	// double-delete races by requiring the existing lifecycle to be RUNNING
	// or COMPLETE.
	SoftDeleteMediaAndEnqueue(ctx context.Context, tenantID, mediaID string, retention time.Duration, outbox OutboxRow, now time.Time) error

	// InitPresignedUpload writes Media + pending Asset + idempotency claim in
	// a single TransactWriteItems. attribute_not_exists on each row protects
	// against double-submit; the claim row makes the operation idempotent on
	// retries that share the same (tenantId, idempotencyKey).
	InitPresignedUpload(ctx context.Context, m media.Media, a media.Asset, idempotencyScope, idempotencyInputHash string, claimTTL time.Duration) (media.Media, media.Asset, error)

	// CompletePresignedUpload moves Asset → COMPLETE, Media → RUNNING, and
	// stages a media.v1.process outbox row alongside an IDEMPOTENCY claim row
	// in one transaction. Used by both the API completion path and the
	// S3-event completion path so they converge on a single row mutation. On
	// claim collision (same scope, same input hash) the method is a no-op; on
	// scope collision with a different hash it errors so the caller knows a
	// different attempt is racing.
	CompletePresignedUpload(ctx context.Context, a media.Asset, mediaID, tenantID string, outbox OutboxRow, claimScope, claimInputHash string, claimTTL time.Duration, now time.Time) error

	// FailPresignedUpload marks Media + Asset FAILED and stages a cleanup
	// outbox row carrying the S3 key. Used for size-cap rejections and the
	// stale-PENDING reaper.
	FailPresignedUpload(ctx context.Context, tenantID, mediaID, assetID string, cleanup OutboxRow, now time.Time) error

	// PutAssetIfAbsent creates the asset row only when no row exists at that
	// key. Used by the derive worker so at-least-once redelivery converges on
	// the same row instead of duplicating derivative assets.
	PutAssetIfAbsent(ctx context.Context, a media.Asset) (inserted bool, err error)

	// CompleteMediaIfProcessing promotes RUNNING → COMPLETE. Already-COMPLETE
	// or DELETED rows are no-ops (do not resurrect a soft-delete).
	CompleteMediaIfProcessing(ctx context.Context, tenantID, mediaID string, now time.Time) error

	// PutImageMetadata persists the width/height/format the image derive step
	// extracts from the original bytes.
	PutImageMetadata(ctx context.Context, meta media.ImageMetadata) error

	// MarkAssetDeleted flips an asset row to DELETED with a TTL. Conditional
	// on the row not already being DELETED so re-deliveries of the delete
	// event collapse to a no-op at the row level.
	MarkAssetDeleted(ctx context.Context, tenantID, mediaID, assetID string, now time.Time) error
}

// MaxRetryAttempts is the upper bound on asset retry attempts. Assets that
// exhaust this budget have failed deterministically; operator recourse is DLQ
// replay rather than automated re-submission.
const MaxRetryAttempts uint32 = 5

// ErrRetryExhausted is returned by RetryAsset when the asset's attempt counter
// has reached MaxRetryAttempts. Callers map this to HTTP 409 Conflict.
var ErrRetryExhausted = errors.New("media: asset retry budget exhausted")

// OutboxRow is the per-stream outbox row written transactionally alongside
// state changes. Type-aliased to the canonical app/outbox.Row so the relay
// builder and producer share one shape.
type OutboxRow = outbox.Row

type StorageQuotaRecorder interface {
	RecordStorageBytes(ctx context.Context, tenantID, mediaID, assetID string, bytes int64) error
}

// Service is the application service. It does not own HTTP framing.
type Service struct {
	Repo    Repository
	Storage Storage
	Quota   StorageQuotaRecorder
	// Derive is the storage-side port CreateAssets requires. Wired explicitly
	// at construction so a misconfigured deployment fails at startup, not on
	// the first /createAssets request.
	Derive DeriveRepository
	Now    func() time.Time // injected for tests
}

func NewService(repo Repository, storage Storage) *Service {
	return &Service{Repo: repo, Storage: storage, Now: func() time.Time { return time.Now().UTC() }}
}
