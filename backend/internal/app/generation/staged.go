package generation

import (
	"context"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// StagedRef points at a provider artifact that the INFER stage wrote to a
// short-lived staging area. DISCLOSURE_POSTPROCESS reads from here, runs the mutation
// pipeline (watermark, EXIF strip, ICC normalize), re-hashes, runs the
// AI-disclosure gate, and only then writes the final asset.
//
// The ref is intentionally self-describing — content type, extension, hash,
// size, and the provider metadata travel with it — so DISCLOSURE_POSTPROCESS doesn't
// need a second DDB read to rebuild context.
type StagedRef struct {
	// StorageKey is the canonical S3 key for the staged bytes. It lives under
	// a short-TTL prefix and is the only place those bytes exist between
	// INFER and DISCLOSURE_POSTPROCESS.
	StorageKey  string            `json:"storage_key"`
	TenantID    string            `json:"tenant_id,omitempty"`
	JobID       string            `json:"job_id,omitempty"`
	ContentType string            `json:"content_type"`
	Extension   string            `json:"extension"`
	SHA256Hex   string            `json:"sha256"`
	SizeBytes   int64             `json:"size_bytes"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	// ExpiresAt is the workflow-side freshness deadline. Past this point the
	// staged bytes may have been GC'd by the S3 lifecycle rule; DISCLOSURE_POSTPROCESS
	// must reject the ref and fail terminally rather than process stale state.
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// StagedArtifactStore is the port the workflow uses for the staged-artifact
// pipeline. PutStaged returns the ref the worker should persist (in the
// idempotency result field); LoadStaged hydrates an Artifact for DISCLOSURE_POSTPROCESS;
// DeleteStaged is best-effort cleanup once the final asset is written.
type StagedArtifactStore interface {
	PutStaged(ctx context.Context, j generation.Job, art generation.Artifact, ttl time.Duration) (StagedRef, error)
	LoadStaged(ctx context.Context, ref StagedRef) (generation.Artifact, error)
	DeleteStaged(ctx context.Context, ref StagedRef) error
}

// ErrStagedNotFound is returned by LoadStaged when the staged bytes are gone
// — typically because the S3 lifecycle rule swept them after ExpiresAt. The
// workflow translates this into a terminal STAGED_EXPIRED so the job fails
// fast rather than waiting for SQS retries to exhaust.
var ErrStagedNotFound = errors.New("staged artifact: not found")

// StagingKey is the S3 key under which staged generation artifacts live.
// Sits under stagingKeyPrefix so the existing bucket lifecycle rule expires
// the object automatically. Exported so the DDB-backed staging impl in
// app/generation/ddb can reuse the same key convention as the in-memory port.
func StagingKey(j generation.Job, ext string) string {
	return stagingKeyPrefix + j.TenantID + "/" + j.MediaID + "/" + j.ID + "." + ext
}

const stagingKeyPrefix = "provider-staging/generation/"
