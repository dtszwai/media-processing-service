// Package derive runs media-worker derivation: image thumbnail generation
// and soft-delete cleanup.
//
// Derivative asset IDs are derived from the inbound MediaEvent.MessageID so
// at-least-once redelivery produces the SAME assetIDs — the Put with
// attribute_not_exists short-circuits to no-op instead of doubling rows or
// re-charging the customer's webhook.
package derive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/imageproc"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// WebhookEnqueuer queues a webhook delivery for the dispatcher.
type WebhookEnqueuer interface {
	EnqueueWebhook(ctx context.Context, env events.WebhookDeliveryEnvelope) error
}

// Handler holds the dependencies needed to process a single MediaEvent.
type Handler struct {
	Repo            mediaapp.Repository
	Storage         mediaapp.Storage
	WebhookEnqueuer WebhookEnqueuer
	Now             func() time.Time
}

func NewHandler(repo mediaapp.Repository, storage mediaapp.Storage, webhookEnq WebhookEnqueuer) *Handler {
	return &Handler{Repo: repo, Storage: storage, WebhookEnqueuer: webhookEnq, Now: func() time.Time { return time.Now().UTC() }}
}

// HandleEvent is the entry point invoked by cmd/workers/media-worker for each
// SQS message.
func (h *Handler) HandleEvent(ctx context.Context, evt events.MediaEvent) error {
	switch evt.EventType {
	case events.EventMediaProcess:
		return h.handleProcess(ctx, evt)
	case events.EventMediaDelete:
		return h.handleDelete(ctx, evt)
	default:
		return nil
	}
}

func (h *Handler) handleProcess(ctx context.Context, evt events.MediaEvent) error {
	m, err := h.Repo.GetMedia(ctx, evt.TenantID, evt.MediaID)
	if err != nil {
		return fmt.Errorf("derive: get media: %w", err)
	}
	orig, err := h.Repo.GetAsset(ctx, evt.TenantID, evt.MediaID, m.OriginalAssetID)
	if err != nil {
		return fmt.Errorf("derive: get original asset: %w", err)
	}

	body, err := h.Storage.Get(ctx, orig.StorageKey)
	if err != nil {
		return fmt.Errorf("derive: get original bytes: %w", err)
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, 200*1024*1024))
	if err != nil {
		return fmt.Errorf("derive: read original: %w", err)
	}

	var derived []media.Asset
	if m.Type == media.TypeImage {
		assets, err := h.deriveImage(ctx, *m, orig, raw, evt.MessageID)
		if err != nil {
			return err
		}
		derived = assets
	}

	now := h.Now()
	if err := h.completeMedia(ctx, *m, now); err != nil {
		return fmt.Errorf("derive: complete media: %w", err)
	}
	m.Lifecycle = media.LifecycleComplete
	m.UpdatedAt = now

	if m.WebhookURL != "" && h.WebhookEnqueuer != nil {
		// Both ids are deterministic functions of the inbound MessageID so
		// at-least-once redelivery of this derive event produces an identical
		// envelope. The webhook dispatcher's idempotency claim is keyed on
		// the delivery id; a repeat arrival therefore short-circuits at the
		// claim and never re-POSTs to the customer's endpoint.
		eventID := evt.MessageID + ".completed"
		deliveryID := evt.MessageID + ".delivery"
		payload := buildCompletedPayload(*m, derived, eventID, now)
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("derive: marshal payload: %w", err)
		}
		env := events.WebhookDeliveryEnvelope{
			DeliveryID: deliveryID,
			TenantID:   m.TenantID,
			MediaID:    m.ID,
			EventID:    eventID,
			EventType:  events.EventMediaCompleted,
			WebhookURL: m.WebhookURL,
			Payload:    bodyBytes,
			CreatedAt:  now,
		}
		if err := h.WebhookEnqueuer.EnqueueWebhook(ctx, env); err != nil {
			return fmt.Errorf("derive: enqueue webhook: %w", err)
		}
	}
	return nil
}

// handleDelete fetches every asset for the media, hard-deletes the S3 object,
// and flips the row to DELETED with a 90d ttl_epoch. The S3 delete and DDB
// asset-lifecycle flip converge on this single worker so the two never drift.
//
// Per-asset ordering: S3 delete first, then the row flip. A transient S3
// failure is returned immediately so SQS redelivers — the row is left in its
// prior lifecycle rather than being tombstoned ahead of the bytes. An S3 404
// (object already absent — typical for assets that never finished uploading)
// is treated as success and the row is still flipped to DELETED: the contract
// is "this asset's bytes are gone and the row is tombstoned"; if bytes never
// existed, recording the tombstone is the correct end state.
//
// The conditional `lifecycle <> :deleted` inside MarkAssetDeleted collapses
// already-DELETED rows to a no-op so the whole handler is idempotent under
// at-least-once SQS redelivery.
func (h *Handler) handleDelete(ctx context.Context, evt events.MediaEvent) error {
	assets, err := h.Repo.ListAssets(ctx, evt.TenantID, evt.MediaID)
	if err != nil {
		return err
	}
	now := h.Now()
	for _, a := range assets {
		if a.Lifecycle == media.AssetLifecycleDeleted {
			continue
		}
		if a.StorageKey != "" {
			if err := h.Storage.Delete(ctx, a.StorageKey); err != nil {
				return fmt.Errorf("derive: s3 delete %s: %w", a.StorageKey, err)
			}
		}
		if err := h.Repo.MarkAssetDeleted(ctx, a.TenantID, a.MediaID, a.ID, now); err != nil {
			return fmt.Errorf("derive: mark asset deleted %s/%s: %w", a.MediaID, a.ID, err)
		}
	}
	return nil
}

// derivedAssetSpec is the per-role record persistDerivedAsset needs. Keeps
// the call sites uniform so adding a new derived role is a one-struct change.
type derivedAssetSpec struct {
	roleTag     string
	role        media.AssetRole
	operation   media.AssetOperation
	contentType string
	extension   string
	body        []byte
}

func (h *Handler) deriveImage(ctx context.Context, m media.Media, orig *media.Asset, raw []byte, messageID string) ([]media.Asset, error) {
	thumb, meta, err := imageproc.ThumbnailPNG(raw, 256)
	if err != nil {
		return nil, fmt.Errorf("derive: image thumbnail: %w", err)
	}
	now := h.Now()
	if err := h.Repo.PutImageMetadata(ctx, media.ImageMetadata{
		TenantID:    m.TenantID,
		MediaID:     m.ID,
		AssetID:     orig.ID,
		Width:       meta.Width,
		Height:      meta.Height,
		Format:      meta.Format,
		ContentType: imageproc.ContentTypeForFormat(meta.Format),
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		return nil, fmt.Errorf("derive: persist image metadata: %w", err)
	}
	a, err := h.persistDerivedAsset(ctx, m, messageID, derivedAssetSpec{
		roleTag:     "thumbnail",
		role:        media.AssetRoleThumbnail,
		operation:   media.AssetOperationImageThumbnail,
		contentType: "image/png",
		extension:   "png",
		body:        thumb,
	}, h.Now())
	if err != nil {
		return nil, err
	}
	return []media.Asset{a}, nil
}

func (h *Handler) persistDerivedAsset(ctx context.Context, m media.Media, messageID string, spec derivedAssetSpec, now time.Time) (media.Asset, error) {
	a := media.Asset{
		ID:          mediaapp.DeriveAssetID(messageID, m.ID, spec.roleTag),
		MediaID:     m.ID,
		TenantID:    m.TenantID,
		Kind:        media.AssetKindDerived,
		Role:        spec.role,
		Operation:   spec.operation,
		Lifecycle:   media.AssetLifecycleProcessing,
		ContentType: spec.contentType,
		Extension:   spec.extension,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	a.StorageKey = media.StorageKey(m.TenantID, m.ID, a.ID, spec.extension)
	put, err := h.Storage.Put(ctx, storage.PutInput{
		Key:         a.StorageKey,
		Body:        bytes.NewReader(spec.body),
		ContentType: spec.contentType,
		SizeBytes:   int64(len(spec.body)),
		Tags: map[string]string{
			"tenant_id": m.TenantID,
			"media_id":  m.ID,
			"asset_id":  a.ID,
			"role":      spec.roleTag,
		},
	})
	if err != nil {
		return media.Asset{}, fmt.Errorf("derive: put %s: %w", spec.roleTag, err)
	}
	a.SizeBytes = uint64(put.SizeBytes)
	a.SHA256 = put.SHA256Hex
	a.Lifecycle = media.AssetLifecycleComplete
	stored, err := h.putAssetIfAbsent(ctx, a)
	if err != nil {
		return media.Asset{}, fmt.Errorf("derive: persist %s asset: %w", spec.roleTag, err)
	}
	return stored, nil
}

func (h *Handler) putAssetIfAbsent(ctx context.Context, a media.Asset) (media.Asset, error) {
	inserted, err := h.Repo.PutAssetIfAbsent(ctx, a)
	if err != nil {
		return media.Asset{}, err
	}
	if inserted {
		return a, nil
	}
	existing, err := h.Repo.GetAsset(ctx, a.TenantID, a.MediaID, a.ID)
	if err != nil {
		return media.Asset{}, err
	}
	return *existing, nil
}

func (h *Handler) completeMedia(ctx context.Context, m media.Media, now time.Time) error {
	return h.Repo.CompleteMediaIfProcessing(ctx, m.TenantID, m.ID, now)
}

func buildCompletedPayload(m media.Media, derived []media.Asset, eventID string, now time.Time) events.MediaCompletedPayload {
	assets := make([]events.CompletedAsset, 0, len(derived))
	for _, a := range derived {
		assets = append(assets, events.CompletedAsset{
			AssetID:     a.ID,
			Role:        string(a.Role),
			Kind:        string(a.Kind),
			Operation:   string(a.Operation),
			ContentType: a.ContentType,
			Extension:   a.Extension,
			SizeBytes:   a.SizeBytes,
			SHA256:      a.SHA256,
		})
	}
	return events.MediaCompletedPayload{
		EventID:   eventID,
		EventType: events.EventMediaCompleted,
		TenantID:  m.TenantID,
		MediaID:   m.ID,
		MediaType: string(m.Type),
		Lifecycle: string(m.Lifecycle),
		Assets:    assets,
		CreatedAt: now,
	}
}
