package media

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	webhookapp "github.com/dtszwai/media-processing-service/backend/internal/app/webhook"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// InitPresignedUpload allocates Media+Asset and returns the presigned URL.
// The Media+Asset+Claim are written in one TransactWriteItems by the repo.
func (s *Service) InitPresignedUpload(ctx context.Context, in InitInput) (*InitOutput, error) {
	if in.TenantID == "" {
		return nil, fmt.Errorf("%w: media.Init: tenant_id required", ErrInvalidInput)
	}
	if in.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: media.Init: idempotency_key required", ErrInvalidInput)
	}
	if in.SizeBytes > MaxPresignedUpload {
		return nil, fmt.Errorf("%w: media.Init: size %d exceeds %d cap", ErrInvalidInput, in.SizeBytes, MaxPresignedUpload)
	}
	contentType, mediaType, ext, err := classifyUploadContentType(in.ContentType)
	if err != nil {
		return nil, fmt.Errorf("media.Init: %w", err)
	}
	in.ContentType = contentType
	webhookURL, err := webhookapp.NormalizeURL(in.WebhookURL)
	if err != nil {
		return nil, fmt.Errorf("media.Init: %w", err)
	}
	in.WebhookURL = webhookURL
	mediaID := "med_" + randid.New()
	assetID := "ast_" + randid.New()
	storageKey := media.StorageKey(in.TenantID, mediaID, assetID, ext)
	now := s.Now()

	m := media.Media{
		ID:              mediaID,
		TenantID:        in.TenantID,
		OwnerUserID:     in.OwnerUserID,
		Visibility:      media.DefaultVisibility(in.OwnerUserID),
		Origin:          media.OriginUpload,
		Type:            mediaType,
		Lifecycle:       media.LifecyclePending,
		OriginalAssetID: assetID,
		WebhookURL:      webhookURL,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	a := media.Asset{
		ID:          assetID,
		MediaID:     mediaID,
		TenantID:    in.TenantID,
		Kind:        media.AssetKindOriginal,
		Role:        media.AssetRoleOriginal,
		Lifecycle:   media.AssetLifecyclePendingUpload,
		StorageKey:  storageKey,
		ContentType: in.ContentType,
		Extension:   ext,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	scope := "UPLOAD_INIT#" + in.TenantID + "#" + in.IdempotencyKey
	inputHash := hashInitInput(in)

	persistedMedia, persistedAsset, err := s.Repo.InitPresignedUpload(ctx, m, a, scope, inputHash, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("media.Init: %w", err)
	}

	url, err := s.Storage.PresignPut(ctx, persistedAsset.StorageKey, persistedAsset.ContentType, PresignTTL)
	if err != nil {
		return nil, fmt.Errorf("media.Init: presign put: %w", err)
	}
	return &InitOutput{
		MediaID:    persistedMedia.ID,
		AssetID:    persistedAsset.ID,
		StorageKey: persistedAsset.StorageKey,
		UploadURL:  url,
		Method:     "PUT",
		Headers:    map[string]string{"Content-Type": persistedAsset.ContentType},
		ExpiresIn:  int(PresignTTL.Seconds()),
		ExpiresAt:  s.Now().Add(PresignTTL),
	}, nil
}

// RefreshPresignedUpload re-presigns the same asset. Requires the media to be
// PENDING and the asset to still be PENDING_UPLOAD so we never re-presign a
// completed or failed upload.
func (s *Service) RefreshPresignedUpload(ctx context.Context, tenantID, mediaID string) (*InitOutput, error) {
	m, err := s.Repo.GetMedia(ctx, tenantID, mediaID)
	if err != nil {
		return nil, err
	}
	if m.Lifecycle != media.LifecyclePending {
		return nil, fmt.Errorf("%w: media.Refresh: media lifecycle %s, expected PENDING", ErrPreconditionFailed, m.Lifecycle)
	}
	a, err := s.Repo.GetAsset(ctx, tenantID, mediaID, m.OriginalAssetID)
	if err != nil {
		return nil, err
	}
	if a.Lifecycle != media.AssetLifecyclePendingUpload {
		return nil, fmt.Errorf("%w: media.Refresh: asset lifecycle %s, expected PENDING_UPLOAD", ErrPreconditionFailed, a.Lifecycle)
	}
	url, err := s.Storage.PresignPut(ctx, a.StorageKey, a.ContentType, PresignTTL)
	if err != nil {
		return nil, err
	}
	return &InitOutput{
		MediaID:    mediaID,
		AssetID:    a.ID,
		StorageKey: a.StorageKey,
		UploadURL:  url,
		Method:     "PUT",
		Headers:    map[string]string{"Content-Type": a.ContentType},
		ExpiresIn:  int(PresignTTL.Seconds()),
		ExpiresAt:  s.Now().Add(PresignTTL),
	}, nil
}

func hashInitInput(in InitInput) string {
	return idempotency.HashInputs(
		in.TenantID,
		in.IdempotencyKey,
		in.ContentType,
		in.Filename,
		strconv.FormatInt(in.SizeBytes, 10),
		in.WebhookURL,
		in.OwnerUserID,
	)
}
