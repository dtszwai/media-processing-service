package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

// CompletePresignedUpload is the API-driven completion path: HEAD the S3
// object so the service has authoritative metadata, then delegate to the
// shared core. Both completion paths converge on completeUploadCore so a
// slow API call and a fast S3 ObjectCreated event cannot double-charge
// storage or double-emit the derivation event.
func (s *Service) CompletePresignedUpload(ctx context.Context, in CompleteInput) (*CompleteOutput, error) {
	if in.TenantID == "" || in.MediaID == "" {
		return nil, fmt.Errorf("%w: tenant_id and media_id required", ErrInvalidInput)
	}
	m, err := s.Repo.GetMedia(ctx, in.TenantID, in.MediaID)
	if err != nil {
		return nil, err
	}
	if m.Lifecycle != media.LifecyclePending {
		// Already converged via the other path (or a previous attempt). Return
		// the persisted shape so /upload/complete callers see a stable result.
		return s.replayCompleteOutput(ctx, in.TenantID, in.MediaID, m)
	}
	a, err := s.Repo.GetAsset(ctx, in.TenantID, in.MediaID, m.OriginalAssetID)
	if err != nil {
		return nil, err
	}
	attrs, err := s.Storage.GetObjectAttributes(ctx, a.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("media.Complete: get object attributes: %w", err)
	}
	return s.completeUploadCore(ctx, m, a, completeCoreInput{
		SizeBytes:        attrs.SizeBytes,
		ContentType:      attrs.ContentType,
		ETag:             attrs.ETag,
		SHA256Hex:        attrs.SHA256Hex,
		StorageVersionID: attrs.VersionID,
		Source:           "api",
	})
}

// CompleteUploadFromS3 is the event-driven completion path. The S3 event names
// the object, but the service still reads authoritative object attributes
// before completing so missing event metadata cannot bypass upload validation.
func (s *Service) CompleteUploadFromS3(ctx context.Context, in S3CompleteInput) (*CompleteOutput, error) {
	if in.TenantID == "" || in.MediaID == "" || in.AssetID == "" || in.StorageKey == "" {
		return nil, fmt.Errorf("%w: tenant_id, media_id, asset_id, storage_key required", ErrInvalidInput)
	}
	m, err := s.Repo.GetMedia(ctx, in.TenantID, in.MediaID)
	if err != nil {
		return nil, err
	}
	if m.Lifecycle != media.LifecyclePending {
		return s.replayCompleteOutput(ctx, in.TenantID, in.MediaID, m)
	}
	a, err := s.Repo.GetAsset(ctx, in.TenantID, in.MediaID, in.AssetID)
	if err != nil {
		return nil, err
	}
	// The S3 event names a different key than the row claims → either a
	// misrouted notification or an attacker probing for an asset they don't own.
	// Reject loudly rather than mutating an unrelated asset's metadata.
	if a.StorageKey != in.StorageKey {
		return nil, fmt.Errorf("media.CompleteFromS3: storage_key mismatch for asset %s", in.AssetID)
	}
	attrs, err := s.Storage.GetObjectAttributes(ctx, a.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("media.CompleteFromS3: get object attributes: %w", err)
	}
	if in.SizeBytes > 0 && attrs.SizeBytes != in.SizeBytes {
		return nil, fmt.Errorf("%w: s3 event size %d does not match object size %d", ErrInvalidInput, in.SizeBytes, attrs.SizeBytes)
	}
	if in.ETag != "" && attrs.ETag != "" && attrs.ETag != in.ETag {
		return nil, fmt.Errorf("%w: s3 event etag %q does not match object etag %q", ErrInvalidInput, in.ETag, attrs.ETag)
	}
	if in.StorageVersionID != "" && attrs.VersionID != "" && attrs.VersionID != in.StorageVersionID {
		return nil, fmt.Errorf("%w: s3 event version_id %q does not match object version_id %q", ErrInvalidInput, in.StorageVersionID, attrs.VersionID)
	}
	if attrs.SHA256Hex == "" {
		attrs.SHA256Hex = in.SHA256Hex
	}
	if attrs.VersionID == "" {
		attrs.VersionID = in.StorageVersionID
	}
	if attrs.ETag == "" {
		attrs.ETag = in.ETag
	}
	return s.completeUploadCore(ctx, m, a, completeCoreInput{
		SizeBytes:        attrs.SizeBytes,
		ContentType:      attrs.ContentType,
		ETag:             attrs.ETag,
		SHA256Hex:        attrs.SHA256Hex,
		StorageVersionID: attrs.VersionID,
		Source:           "s3-event",
	})
}

// completeCoreInput is the per-call metadata both completion paths feed into
// the shared core. The fields are already-validated S3 metadata; the core
// doesn't re-derive any of them.
type completeCoreInput struct {
	SizeBytes        int64
	ContentType      string
	ETag             string
	SHA256Hex        string
	StorageVersionID string
	Source           string // "api" | "s3-event" — telemetry only
}

// completeUploadCore is the single transition point for upload completion.
// Both the API path and the S3-event path call this so the claim scope is
// guaranteed identical and a race between them collapses to one row mutation.
//
// The claim scope is `UPLOAD_COMPLETE#<tenant>#<asset_id>#<version-or-etag>`:
// keyed on the storage instance, not on the request, so a re-upload to a new
// version transitions independently while two completion attempts on the same
// uploaded bytes converge.
func (s *Service) completeUploadCore(ctx context.Context, m *media.Media, a *media.Asset, in completeCoreInput) (*CompleteOutput, error) {
	now := s.Now()

	if in.SizeBytes > MaxPresignedUpload {
		// Size-cap rejection — failed-state-first, async cleanup.
		cleanup := OutboxRow{
			Stream:      outbox.StreamMediaCleanup,
			PartitionTS: now,
			Shard:       shardkey.Of(m.ID, 8),
			EventID:     randid.New(),
			Body:        buildCleanupOutboxBody(m.TenantID, m.ID, a.ID, a.StorageKey, ""),
			EventType:   string(events.EventMediaFailed),
			TenantID:    m.TenantID,
			Reason:      "SIZE_CAP_EXCEEDED",
		}
		if ferr := s.Repo.FailPresignedUpload(ctx, m.TenantID, m.ID, a.ID, cleanup, now); ferr != nil {
			return nil, fmt.Errorf("media.Complete: fail transaction: %w", ferr)
		}
		return nil, fmt.Errorf("%w: uploaded size %d exceeds %d cap", ErrInvalidInput, in.SizeBytes, MaxPresignedUpload)
	}

	completedContentType, err := completedUploadContentType(a.ContentType, in.ContentType)
	if err != nil {
		cleanup := OutboxRow{
			Stream:      outbox.StreamMediaCleanup,
			PartitionTS: now,
			Shard:       shardkey.Of(m.ID, 8),
			EventID:     randid.New(),
			Body:        buildCleanupOutboxBody(m.TenantID, m.ID, a.ID, a.StorageKey, "UNSUPPORTED_MEDIA_TYPE"),
			EventType:   string(events.EventMediaFailed),
			TenantID:    m.TenantID,
			Reason:      "UNSUPPORTED_MEDIA_TYPE",
		}
		if ferr := s.Repo.FailPresignedUpload(ctx, m.TenantID, m.ID, a.ID, cleanup, now); ferr != nil {
			return nil, fmt.Errorf("media.Complete: fail transaction: %w", ferr)
		}
		return nil, fmt.Errorf("media.Complete: %w", err)
	}

	a.SizeBytes = uint64(in.SizeBytes)
	a.ContentType = completedContentType
	if in.ETag != "" {
		a.ETag = in.ETag
	}
	if in.SHA256Hex != "" {
		a.SHA256 = in.SHA256Hex
	} else {
		// S3 did not surface a checksum — client may not have sent
		// x-amz-checksum-sha256. The derive worker computes SHA-256 from the
		// object bytes as a fallback rather than blocking the completion FSM on
		// a streaming GetObject hash here, which would double-charge bandwidth
		// on every upload.
		slog.WarnContext(ctx, "media.Complete: no SHA-256 from S3; derive worker will recompute",
			"tenant_id", m.TenantID, "media_id", m.ID, "asset_id", a.ID, "source", in.Source)
	}
	a.Lifecycle = media.AssetLifecycleComplete
	a.UpdatedAt = now

	processEvt := events.MediaEvent{
		MessageID:   randid.New(),
		EventType:   events.EventMediaProcess,
		TenantID:    m.TenantID,
		MediaID:     m.ID,
		AssetID:     a.ID,
		Traceparent: extractTraceparent(ctx),
		CreatedAt:   now,
	}
	body, _ := json.Marshal(processEvt)
	row := OutboxRow{
		Stream:      outbox.StreamMedia,
		PartitionTS: now,
		Shard:       shardkey.Of(m.ID, 8),
		EventID:     processEvt.MessageID,
		Body:        body,
		EventType:   string(events.EventMediaProcess),
		TenantID:    m.TenantID,
	}

	scope := claimScopeFor(m.TenantID, a.ID, in.StorageVersionID, in.ETag)
	inputHash := idempotency.HashInputs(
		m.ID,
		a.ID,
		a.StorageKey,
		strconv.FormatInt(in.SizeBytes, 10),
	)
	if err := s.Repo.CompletePresignedUpload(ctx, *a, m.ID, m.TenantID, row, scope, inputHash, claimTTL, now); err != nil {
		return nil, fmt.Errorf("media.Complete: success transaction: %w", err)
	}
	if err := s.recordStorageBytes(ctx, m.TenantID, m.ID, a.ID, in.SizeBytes); err != nil {
		return nil, err
	}

	return &CompleteOutput{
		MediaID:     m.ID,
		AssetID:     a.ID,
		Lifecycle:   string(media.LifecycleRunning),
		SizeBytes:   in.SizeBytes,
		ContentType: completedContentType,
		ETag:        a.ETag,
	}, nil
}

// claimTTL bounds the IDEMPOTENCY#UPLOAD_COMPLETE row. 24 hours covers the
// outermost retry horizon (matching the stale-PENDING reaper window in
// reaper.go) — after that the claim row TTL-sweeps; a fresh re-upload then
// gets a fresh scope keyed off the new version id.
const claimTTL = 24 * time.Hour

// claimScopeFor builds the upload-completion claim scope keyed on the storage
// instance: `UPLOAD_COMPLETE#<tenant>#<asset>#<vid>`.
// The bucket has versioning enabled (terraform/modules/s3) so the S3 version
// id is the per-PUT discriminator; ETag is its content-equivalent when the
// adapter stack happens not to surface a version (e.g. test fakes). The
// asset id is the last-resort discriminator: it still gives a valid scope at
// the cost of collapsing a hypothetical re-upload under the same key into the
// prior claim — a property only test fakes ever observe.
func claimScopeFor(tenantID, assetID, versionID, etag string) string {
	disc := versionID
	if disc == "" {
		disc = etag
	}
	if disc == "" {
		disc = assetID
	}
	return "UPLOAD_COMPLETE#" + tenantID + "#" + assetID + "#" + disc
}

func completedUploadContentType(assetContentType, observedContentType string) (string, error) {
	assetContentType, _, _, err := classifyUploadContentType(assetContentType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(observedContentType) == "" {
		return "", fmt.Errorf("%w: uploaded content_type missing for presigned content_type %q", ErrInvalidInput, assetContentType)
	}
	observedContentType, _, _, err = classifyUploadContentType(observedContentType)
	if err != nil {
		return "", err
	}
	if observedContentType != assetContentType {
		return "", fmt.Errorf("%w: uploaded content_type %q does not match presigned content_type %q", ErrInvalidInput, observedContentType, assetContentType)
	}
	return observedContentType, nil
}

// replayCompleteOutput returns a CompleteOutput shape for a row that has
// already left PENDING. Both completion paths land here when they
// race — the second caller sees the same response as the first.
func (s *Service) replayCompleteOutput(ctx context.Context, tenantID, mediaID string, m *media.Media) (*CompleteOutput, error) {
	a, assetErr := s.Repo.GetAsset(ctx, tenantID, mediaID, m.OriginalAssetID)
	if assetErr != nil {
		return &CompleteOutput{MediaID: mediaID, AssetID: m.OriginalAssetID, Lifecycle: string(m.Lifecycle)}, nil
	}
	if err := s.recordStorageBytes(ctx, tenantID, mediaID, a.ID, int64(a.SizeBytes)); err != nil {
		return nil, err
	}
	return &CompleteOutput{
		MediaID:     mediaID,
		AssetID:     a.ID,
		Lifecycle:   string(m.Lifecycle),
		SizeBytes:   int64(a.SizeBytes),
		ContentType: a.ContentType,
		ETag:        a.ETag,
	}, nil
}

func (s *Service) recordStorageBytes(ctx context.Context, tenantID, mediaID, assetID string, bytes int64) error {
	if s.Quota == nil || bytes <= 0 {
		return nil
	}
	if err := s.Quota.RecordStorageBytes(ctx, tenantID, mediaID, assetID, bytes); err != nil {
		return fmt.Errorf("media.Complete: record storage quota: %w", err)
	}
	return nil
}

// buildCleanupOutboxBody serializes the media-cleanup outbox payload. The
// cleanup-worker reads tenant_id / media_id / asset_id / storage_key off the
// body; reason is informational telemetry and omitted when empty.
func buildCleanupOutboxBody(tenantID, mediaID, assetID, storageKey, reason string) []byte {
	fields := map[string]string{
		"event_type":  string(events.EventMediaCleanup),
		"tenant_id":   tenantID,
		"media_id":    mediaID,
		"asset_id":    assetID,
		"storage_key": storageKey,
	}
	if reason != "" {
		fields["reason"] = reason
	}
	body, _ := json.Marshal(fields)
	return body
}
