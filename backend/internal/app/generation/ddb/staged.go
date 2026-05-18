package ddb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// Staging persists staged artifacts via S3 (bytes) + DynamoDB (tracking row).
// The S3 key sits under `provider-staging/` so the bucket-level lifecycle rule
// expires the object after 24h regardless of whether DDB cleanup ran. The
// tracking row carries the same expiry as a DDB TTL attribute so dangling
// metadata also dies.
type Staging struct {
	KV   kv.KV
	Blob storage.Storage
	Now  func() time.Time
}

func NewStaging(k kv.KV, blob storage.Storage) *Staging {
	return &Staging{KV: k, Blob: blob, Now: func() time.Time { return time.Now().UTC() }}
}

const stagedSK = "ARTIFACT"

func (s *Staging) PutStaged(ctx context.Context, j generation.Job, art generation.Artifact, ttl time.Duration) (genapp.StagedRef, error) {
	if j.ID == "" || j.TenantID == "" || j.MediaID == "" {
		return genapp.StagedRef{}, errors.New("ddb staging: tenant + media + job id required")
	}
	ext := art.Extension
	if ext == "" {
		ext = "bin"
	}
	now := s.now()
	expires := now.Add(ttl)
	key := genapp.StagingKey(j, ext)

	put, err := s.Blob.Put(ctx, storage.PutInput{
		Key:         key,
		Body:        bytes.NewReader(art.Bytes),
		ContentType: art.ContentType,
		SizeBytes:   int64(len(art.Bytes)),
		SHA256Hex:   art.SHA256,
		Tags: map[string]string{
			"tenant_id": j.TenantID,
			"media_id":  j.MediaID,
			"job_id":    j.ID,
			"origin":    "generation-staged",
		},
		Metadata: art.Metadata,
	})
	if err != nil {
		return genapp.StagedRef{}, fmt.Errorf("ddb staging: s3 put: %w", err)
	}

	ref := genapp.StagedRef{
		StorageKey:  put.Key,
		TenantID:    j.TenantID,
		JobID:       j.ID,
		ContentType: art.ContentType,
		Extension:   ext,
		SHA256Hex:   put.SHA256Hex,
		SizeBytes:   put.SizeBytes,
		Metadata:    maps.Clone(art.Metadata),
		CreatedAt:   now,
		ExpiresAt:   expires,
	}

	item := map[string]any{
		"PK":           StagedPK(j.TenantID, j.ID),
		"SK":           stagedSK,
		"tenant_id":    j.TenantID,
		"job_id":       j.ID,
		"media_id":     j.MediaID,
		"storage_key":  ref.StorageKey,
		"content_type": ref.ContentType,
		"extension":    ref.Extension,
		"sha256":       ref.SHA256Hex,
		"size_bytes":   ref.SizeBytes,
		"metadata":     ref.Metadata,
		"created_at":   ref.CreatedAt.Format(time.RFC3339Nano),
		"expires_at":   ref.ExpiresAt.Format(time.RFC3339Nano),
		// DDB TTL attribute — unix seconds; the table's TTL config keys off
		// this and deletes the row asynchronously after the deadline.
		"ttl_epoch": ref.ExpiresAt.Unix(),
	}
	if err := s.KV.Put(ctx, item, kv.PutOptions{}); err != nil {
		// Best-effort rollback of the staged bytes if the tracking row failed
		// to write. Otherwise we leak the S3 object until lifecycle GC.
		_ = s.Blob.Delete(ctx, put.Key)
		return genapp.StagedRef{}, fmt.Errorf("ddb staging: ddb put: %w", err)
	}
	return ref, nil
}

func (s *Staging) LoadStaged(ctx context.Context, ref genapp.StagedRef) (generation.Artifact, error) {
	if ref.StorageKey == "" {
		return generation.Artifact{}, errors.New("ddb staging: ref.StorageKey required")
	}
	if ref.TenantID == "" || ref.JobID == "" {
		return generation.Artifact{}, errors.New("ddb staging: ref.TenantID and ref.JobID required to hydrate metadata")
	}
	// Tracking row is the canonical source for provider metadata; callers
	// typically reconstruct ref from just StorageKey on the replay path.
	var row stagedRow
	if err := s.KV.Get(ctx, kv.Key{PK: StagedPK(ref.TenantID, ref.JobID), SK: stagedSK}, &row); err != nil {
		if errors.Is(err, kv.ErrNotFound) {
			return generation.Artifact{}, genapp.ErrStagedNotFound
		}
		return generation.Artifact{}, fmt.Errorf("ddb staging: ddb get tracking row: %w", err)
	}
	if row.Expired(s.now()) {
		return generation.Artifact{}, genapp.ErrStagedNotFound
	}

	rc, err := s.Blob.Get(ctx, ref.StorageKey)
	if err != nil {
		if isS3NotFound(err) {
			return generation.Artifact{}, genapp.ErrStagedNotFound
		}
		return generation.Artifact{}, fmt.Errorf("ddb staging: s3 get: %w", err)
	}
	defer rc.Close()
	buf, err := io.ReadAll(rc)
	if err != nil {
		return generation.Artifact{}, fmt.Errorf("ddb staging: s3 read: %w", err)
	}
	return generation.Artifact{
		Bytes:       buf,
		ContentType: row.ContentType,
		Extension:   row.Extension,
		SHA256:      row.SHA256,
		Metadata:    maps.Clone(row.Metadata),
	}, nil
}

// stagedRow mirrors the attributes PutStaged writes to the tracking row.
type stagedRow struct {
	ContentType string            `dynamodbav:"content_type"`
	Extension   string            `dynamodbav:"extension"`
	SHA256      string            `dynamodbav:"sha256"`
	Metadata    map[string]string `dynamodbav:"metadata"`
	ExpiresAt   string            `dynamodbav:"expires_at"`
	TTLEpoch    int64             `dynamodbav:"ttl_epoch"`
}

func (r stagedRow) Expired(now time.Time) bool {
	if r.TTLEpoch > 0 {
		return r.TTLEpoch <= now.Unix()
	}
	if r.ExpiresAt == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	if err != nil {
		return false
	}
	return !expiresAt.After(now)
}

func (s *Staging) DeleteStaged(ctx context.Context, ref genapp.StagedRef) error {
	if ref.StorageKey == "" {
		return nil
	}
	// S3 first so partial failure still removes the bytes (the expensive
	// thing) before we drop the tracking row.
	if err := s.Blob.Delete(ctx, ref.StorageKey); err != nil && !isS3NotFound(err) {
		return fmt.Errorf("ddb staging: s3 delete: %w", err)
	}
	if ref.TenantID != "" && ref.JobID != "" {
		_ = s.KV.Delete(ctx, kv.DeleteOp{Key: kv.Key{PK: StagedPK(ref.TenantID, ref.JobID), SK: stagedSK}})
	}
	return nil
}

func (s *Staging) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// S3 not-found surfaces as NoSuchKey or 404. We match by string rather
	// than typed errors so this works for both LocalStack and AWS without
	// pulling smithy types into the app layer.
	return strings.Contains(s, "NoSuchKey") ||
		strings.Contains(s, "NotFound") ||
		strings.Contains(s, "status code: 404")
}
