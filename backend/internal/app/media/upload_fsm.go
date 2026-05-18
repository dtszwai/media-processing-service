// Package media — presigned upload FSM (init/refresh/complete).
//
// Flow:
//
//	InitPresignedUpload  client supplies idempotency_key, server allocates
//	  mediaId + assetId, writes Media (PENDING) + Asset (PENDING_UPLOAD)
//	  + idempotency claim in ONE TransactWriteItems, returns a presigned PUT
//	  URL.
//	RefreshPresignedUpload  re-presign the same assetId; requires the media
//	  to be PENDING and the asset to still be in PENDING_UPLOAD. No new
//	  idempotency claim.
//	CompletePresignedUpload  HEAD the S3 object; on size-cap violation flip
//	  Media+Asset to FAILED and stage a cleanup outbox row (S3 delete happens
//	  async via the cleanup worker). On success: Asset → COMPLETE with size +
//	  content type + etag, Media → RUNNING, and stage a media.v1.
//	  process outbox row — all in one transaction.
package media

import "time"

// MaxPresignedUpload is the upper size cap per AGENTS.md (presigned ≤ 1 GB).
const MaxPresignedUpload = 1 * 1024 * 1024 * 1024

// PresignTTL is the lifetime of the presigned PUT URL. Long enough for slow
// uploads, short enough that an abandoned URL doesn't linger.
const PresignTTL = 30 * time.Minute

// InitInput is the shape the API hands to InitPresignedUpload.
type InitInput struct {
	TenantID       string
	OwnerUserID    string
	WebhookURL     string
	Filename       string
	ContentType    string
	SizeBytes      int64 // client-claimed; server verifies at Complete
	IdempotencyKey string
}

// InitOutput is the wire shape returned to the client. method + headers tell
// the client exactly how to PUT to S3 (S3 verifies them on the request).
type InitOutput struct {
	MediaID    string            `json:"mediaId"`
	AssetID    string            `json:"assetId"`
	StorageKey string            `json:"storageKey"`
	UploadURL  string            `json:"uploadUrl"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	ExpiresIn  int               `json:"expiresIn"`
	ExpiresAt  time.Time         `json:"expiresAt"`
}

// CompleteInput closes the upload FSM (client-driven /upload/complete path).
type CompleteInput struct {
	TenantID string
	MediaID  string
}

type CompleteOutput struct {
	MediaID     string `json:"mediaId"`
	AssetID     string `json:"assetId"`
	Lifecycle   string `json:"status"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	ETag        string `json:"etag,omitempty"`
}

// S3CompleteInput is the parsed shape of an S3 ObjectCreated event the
// upload-events worker forwards into the same completion FSM. Mapping from raw
// S3 event JSON into this struct lives in the worker — by the time the service
// sees these fields they have already been validated against the canonical
// storage-key layout and tied back to a (tenant, media, asset) the row store
// knows about.
type S3CompleteInput struct {
	TenantID         string
	MediaID          string
	AssetID          string
	StorageKey       string
	StorageVersionID string
	SizeBytes        int64
	SHA256Hex        string // hex-encoded; "" when S3 didn't surface a checksum
	ContentType      string // S3-reported; "" when absent
	ETag             string // S3 object ETag (no surrounding quotes); "" when absent
}
