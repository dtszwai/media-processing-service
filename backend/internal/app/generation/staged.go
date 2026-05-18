package generation

import (
	"context"
	"errors"
	"maps"
	"sync"
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

// MemStaging is the in-memory StagedArtifactStore used by tests and the
// in-process poller. Keyed by storage key so multiple jobs can stage
// concurrently without collision.
type MemStaging struct {
	mu      sync.Mutex
	objects map[string]stagedObject
	Now     func() time.Time
}

type stagedObject struct {
	bytes []byte
	ref   StagedRef
}

func NewMemStaging() *MemStaging {
	return &MemStaging{
		objects: map[string]stagedObject{},
		Now:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *MemStaging) PutStaged(_ context.Context, j generation.Job, art generation.Artifact, ttl time.Duration) (StagedRef, error) {
	if j.ID == "" || j.TenantID == "" {
		return StagedRef{}, errors.New("mem staging: tenant + job id required")
	}
	ext := art.Extension
	if ext == "" {
		ext = "bin"
	}
	now := s.now()
	ref := StagedRef{
		StorageKey:  StagingKey(j, ext),
		TenantID:    j.TenantID,
		JobID:       j.ID,
		ContentType: art.ContentType,
		Extension:   ext,
		SHA256Hex:   art.SHA256,
		SizeBytes:   int64(len(art.Bytes)),
		Metadata:    maps.Clone(art.Metadata),
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := make([]byte, len(art.Bytes))
	copy(buf, art.Bytes)
	s.objects[ref.StorageKey] = stagedObject{bytes: buf, ref: ref}
	return ref, nil
}

func (s *MemStaging) LoadStaged(_ context.Context, ref StagedRef) (generation.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[ref.StorageKey]
	if !ok {
		return generation.Artifact{}, ErrStagedNotFound
	}
	if !obj.ref.ExpiresAt.IsZero() && !obj.ref.ExpiresAt.After(s.now()) {
		delete(s.objects, ref.StorageKey)
		return generation.Artifact{}, ErrStagedNotFound
	}
	buf := make([]byte, len(obj.bytes))
	copy(buf, obj.bytes)
	// Read attributes from the stored ref (the canonical record), not from
	// the caller's ref — callers reconstruct the ref from just the storage
	// key on the replay path and don't carry the original provider metadata.
	return generation.Artifact{
		Bytes:       buf,
		ContentType: obj.ref.ContentType,
		Extension:   obj.ref.Extension,
		SHA256:      obj.ref.SHA256Hex,
		Metadata:    maps.Clone(obj.ref.Metadata),
	}, nil
}

func (s *MemStaging) DeleteStaged(_ context.Context, ref StagedRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, ref.StorageKey)
	return nil
}

// Drop forcibly evicts the staged bytes so LoadStaged returns ErrStagedNotFound.
// Used by tests to simulate the S3 lifecycle sweep that runs after ExpiresAt.
func (s *MemStaging) Drop(storageKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, storageKey)
}

func (s *MemStaging) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// StagingKey is the S3 key under which staged generation artifacts live.
// Sits under stagingKeyPrefix so the existing bucket lifecycle rule expires
// the object automatically. Exported so the DDB-backed staging impl in
// app/generation/ddb can reuse the same key convention as the in-memory port.
func StagingKey(j generation.Job, ext string) string {
	return stagingKeyPrefix + j.TenantID + "/" + j.MediaID + "/" + j.ID + "." + ext
}

const stagingKeyPrefix = "provider-staging/generation/"
