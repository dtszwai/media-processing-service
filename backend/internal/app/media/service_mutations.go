package media

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

// extractTraceparent reads the W3C traceparent header from the current span
// context via the global propagator. Returns "" when no active span exists.
func extractTraceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// SoftDeleteRetention is the 90-day TTL applied to soft-deleted media rows.
const SoftDeleteRetention = 90 * 24 * time.Hour

// SoftDelete flips Media.Lifecycle to DELETED conditionally and sets a TTL
// matching SoftDeleteRetention. Production repos write the delete event
// through the media outbox in the same transaction.
func (s *Service) SoftDelete(ctx context.Context, tenantID, mediaID string) error {
	now := s.Now()
	evt := events.MediaEvent{
		MessageID:   randid.New(),
		EventType:   events.EventMediaDelete,
		TenantID:    tenantID,
		MediaID:     mediaID,
		Traceparent: extractTraceparent(ctx),
		CreatedAt:   now,
	}
	body, _ := json.Marshal(evt)
	row := OutboxRow{
		Stream:      outbox.StreamMedia,
		PartitionTS: now,
		Shard:       shardkey.Of(mediaID, 8),
		EventID:     evt.MessageID,
		Body:        body,
		EventType:   string(events.EventMediaDelete),
		TenantID:    tenantID,
	}
	return s.Repo.SoftDeleteMediaAndEnqueue(ctx, tenantID, mediaID, SoftDeleteRetention, row, now)
}

// RetryAsset re-enqueues a derive job for a FAILED asset. The asset row is
// atomically flipped FAILED → PROCESSING with attempts++ (conditional so
// concurrent retries collapse), and a media.v1.process outbox row is staged in
// the same transaction so the worker never sees a state change without the
// corresponding event.
//
// The conditional bound on attempts (< MaxRetryAttempts) prevents infinite
// retry loops on assets that fail deterministically. Callers receive
// ErrRetryExhausted in that case; the operator's recourse is DLQ replay.
func (s *Service) RetryAsset(ctx context.Context, tenantID, mediaID, assetID string) (*media.Asset, error) {
	if tenantID == "" || mediaID == "" || assetID == "" {
		return nil, fmt.Errorf("%w: tenant_id, media_id, asset_id required", ErrInvalidInput)
	}
	now := s.Now()
	evtID := randid.New()
	evt := events.MediaEvent{
		MessageID:   evtID,
		EventType:   events.EventMediaProcess,
		TenantID:    tenantID,
		MediaID:     mediaID,
		AssetID:     assetID,
		Traceparent: extractTraceparent(ctx),
		CreatedAt:   now,
	}
	body, _ := json.Marshal(evt)
	row := OutboxRow{
		Stream:      outbox.StreamMedia,
		PartitionTS: now,
		Shard:       shardkey.Of(mediaID, 8),
		EventID:     evtID,
		Body:        body,
		EventType:   string(events.EventMediaProcess),
		TenantID:    tenantID,
	}
	return s.Repo.RetryAsset(ctx, tenantID, mediaID, assetID, MaxRetryAttempts, row, now)
}
