// Package media defines the Media and Asset domain types.
//
// These are the pure domain representations of media items and their derived
// assets — the shapes the rest of the service reasons about regardless of
// how they're serialized. Transport DTOs (protobuf/Connect) and persistence
// rows are mapped from these types in the surrounding layers, never the
// other way around.
package media

import (
	"strings"
	"time"
)

type Origin string

const (
	OriginUpload    Origin = "UPLOAD"
	OriginGenerated Origin = "GENERATED"
)

type Type string

const (
	TypeImage Type = "IMAGE"
	TypeAudio Type = "AUDIO"
)

type Lifecycle string

const (
	LifecyclePending  Lifecycle = "PENDING"
	LifecycleRunning  Lifecycle = "RUNNING"
	LifecycleComplete Lifecycle = "COMPLETE"
	LifecycleFailed   Lifecycle = "FAILED"
	LifecycleDeleted  Lifecycle = "DELETED"
)

type Visibility string

const (
	VisibilityOwnerPrivate Visibility = "OWNER_PRIVATE"
	VisibilityTenantShared Visibility = "TENANT_SHARED"
)

func DefaultVisibility(ownerUserID string) Visibility {
	if ownerUserID != "" {
		return VisibilityOwnerPrivate
	}
	return VisibilityTenantShared
}

type AssetKind string

const (
	AssetKindOriginal  AssetKind = "ORIGINAL"
	AssetKindDerived   AssetKind = "DERIVED"
	AssetKindGenerated AssetKind = "GENERATED"
)

type AssetRole string

const (
	AssetRoleOriginal  AssetRole = "ORIGINAL"
	AssetRoleThumbnail AssetRole = "THUMBNAIL"
	AssetRolePreview   AssetRole = "PREVIEW"
	AssetRoleDownload  AssetRole = "DOWNLOAD"
	AssetRoleFinal     AssetRole = "FINAL"
)

type AssetOperation string

const (
	AssetOperationImageResize     AssetOperation = "IMAGE_RESIZE"
	AssetOperationImageThumbnail  AssetOperation = "IMAGE_THUMBNAIL"
	AssetOperationGenerationFinal AssetOperation = "GENERATION_FINAL"
)

type AssetLifecycle string

const (
	AssetLifecyclePendingUpload AssetLifecycle = "PENDING_UPLOAD"
	AssetLifecyclePending       AssetLifecycle = "PENDING"
	AssetLifecycleProcessing    AssetLifecycle = "PROCESSING"
	AssetLifecycleComplete      AssetLifecycle = "COMPLETE"
	AssetLifecycleFailed        AssetLifecycle = "FAILED"
	AssetLifecycleDeleted       AssetLifecycle = "DELETED"
)

type Provenance struct {
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	AIGenerated bool   `json:"aiGenerated,omitempty"`
	Disclosure  string `json:"disclosure,omitempty"`
	Watermark   string `json:"watermark,omitempty"`
	Safety      string `json:"safety,omitempty"`
}

type DesiredSpec struct {
	OutputFormat string   `json:"outputFormat,omitempty"`
	Width        uint32   `json:"width,omitempty"`
	Height       uint32   `json:"height,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type Media struct {
	ID              string     `json:"mediaId"`
	TenantID        string     `json:"tenantId"`
	OwnerUserID     string     `json:"ownerUserId,omitempty"`
	Visibility      Visibility `json:"visibility"`
	Origin          Origin     `json:"origin"`
	Type            Type       `json:"mediaType"`
	Lifecycle       Lifecycle  `json:"status"`
	OriginalAssetID string     `json:"originalAssetId,omitempty"`
	WebhookURL      string     `json:"webhookUrl,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
}

type ImageMetadata struct {
	TenantID    string    `json:"tenantId"`
	MediaID     string    `json:"mediaId"`
	AssetID     string    `json:"assetId"`
	Width       uint32    `json:"width"`
	Height      uint32    `json:"height"`
	Format      string    `json:"format"`
	ContentType string    `json:"contentType,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Asset struct {
	ID            string         `json:"assetId"`
	MediaID       string         `json:"mediaId"`
	TenantID      string         `json:"tenantId"`
	Kind          AssetKind      `json:"kind"`
	Role          AssetRole      `json:"role"`
	Operation     AssetOperation `json:"operation,omitempty"`
	Lifecycle     AssetLifecycle `json:"status"`
	SourceAssetID string         `json:"sourceAssetId,omitempty"`
	StorageKey    string         `json:"storageKey,omitempty"`
	ContentType   string         `json:"contentType,omitempty"`
	Extension     string         `json:"extension,omitempty"`
	SizeBytes     uint64         `json:"sizeBytes,omitempty"`
	SHA256        string         `json:"sha256,omitempty"`
	ETag          string         `json:"etag,omitempty"`
	DesiredSpec   *DesiredSpec   `json:"desiredSpec,omitempty"`
	Provenance    *Provenance    `json:"provenance,omitempty"`
	Attempts      uint32         `json:"attempts,omitempty"`
	ErrorCode     string         `json:"errorCode,omitempty"`
	ErrorMessage  string         `json:"errorMessage,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// StorageKey returns the canonical S3 object key:
// {tenantID}/{mediaID}/assets/{assetID}[.{ext}].
func StorageKey(tenantID, mediaID, assetID, ext string) string {
	if ext == "" {
		return tenantID + "/" + mediaID + "/assets/" + assetID
	}
	return tenantID + "/" + mediaID + "/assets/" + assetID + "." + ext
}

// ParseStorageKey inverts StorageKey. Returns ok=false when the key shape does
// not match — important on the S3-event ingest path so an upload that landed
// in an unexpected prefix can't poison an unrelated tenant/media row.
//
// Centralising the parse here keeps the upload-ingest worker and any future
// asset-aware consumer aligned with the canonical writer in StorageKey: the
// key layout is owned by one function, and a future shape change touches one
// place rather than every reader.
func ParseStorageKey(key string) (tenantID, mediaID, assetID, ext string, ok bool) {
	// {tenant}/{media}/assets/{assetID}[.{ext}]
	parts := strings.SplitN(key, "/", 4)
	if len(parts) != 4 || parts[2] != "assets" || parts[0] == "" || parts[1] == "" || parts[3] == "" {
		return "", "", "", "", false
	}
	last := parts[3]
	if dot := strings.LastIndex(last, "."); dot > 0 && dot+1 < len(last) {
		return parts[0], parts[1], last[:dot], last[dot+1:], true
	}
	return parts[0], parts[1], last, "", true
}
