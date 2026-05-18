package media

import (
	"context"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

// PresignReady checks all lifecycle guards and returns the asset when it is
// safe to hand out a download URL. Bypasses the visibility check; callers that
// must enforce a Principal use PresignReadyVisible. Used by the generation
// result-asset presigner, which has already authenticated its caller through
// the generation surface.
func (s *Service) PresignReady(ctx context.Context, tenantID, mediaID, assetID string) (*media.Asset, error) {
	return s.presignReady(ctx, Principal{TenantID: tenantID}, mediaID, assetID, false)
}

// PresignReadyVisible is PresignReady plus the per-Principal visibility check
// the customer-facing /download-url path requires.
func (s *Service) PresignReadyVisible(ctx context.Context, p Principal, mediaID, assetID string) (*media.Asset, error) {
	return s.presignReady(ctx, p, mediaID, assetID, true)
}

func (s *Service) PresignAssetDownloadVisible(ctx context.Context, p Principal, mediaID, assetID string, ttl time.Duration) (string, error) {
	a, err := s.PresignReadyVisible(ctx, p, mediaID, assetID)
	if err != nil {
		return "", err
	}
	return s.Storage.PresignGet(ctx, a.StorageKey, ttl)
}

func (s *Service) presignReady(ctx context.Context, p Principal, mediaID, assetID string, enforceVisibility bool) (*media.Asset, error) {
	m, err := s.Repo.GetMedia(ctx, p.TenantID, mediaID)
	if err != nil {
		return nil, err
	}
	if enforceVisibility {
		if err := authorizeRead(p, *m); err != nil {
			return nil, err
		}
	}
	if m.Lifecycle == media.LifecycleDeleted {
		return nil, fmt.Errorf("%w: parent media deleted", ErrPreconditionFailed)
	}
	a, err := s.Repo.GetAsset(ctx, p.TenantID, mediaID, assetID)
	if err != nil {
		return nil, err
	}
	if a.Lifecycle == media.AssetLifecycleDeleted {
		return nil, fmt.Errorf("%w: asset deleted", ErrPreconditionFailed)
	}
	if a.Lifecycle != media.AssetLifecycleComplete {
		return nil, fmt.Errorf("%w: asset not ready", ErrPreconditionFailed)
	}
	if a.StorageKey == "" {
		return nil, fmt.Errorf("%w: asset missing storage_key", ErrPreconditionFailed)
	}
	return a, nil
}

// RoleURL is the response shape for role-keyed selector routes
// (/preview-url, /thumbnail-url, /download-url). The asset id is returned so
// the client can subsequently fetch metadata or replay the lookup against the
// resolved asset directly when needed.
type RoleURL struct {
	AssetID     string    `json:"assetId"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expiresAt"`
	ContentType string    `json:"contentType,omitempty"`
	SizeBytes   uint64    `json:"sizeBytes,omitempty"`
}

// GetRoleURLVisible resolves a role selector (PREVIEW / THUMBNAIL / DOWNLOAD
// / …) to the matching COMPLETE asset and returns a presigned GET URL.
// DOWNLOAD is the only role that opts into ORIGINAL fallback. The
// parent-Media soft-delete guard prevents a tombstoned media from handing
// out fresh URLs even when the role partition still references a COMPLETE
// asset row.
func (s *Service) GetRoleURLVisible(ctx context.Context, p Principal, mediaID string, role media.AssetRole, ttl time.Duration) (*RoleURL, error) {
	if p.TenantID == "" || mediaID == "" || role == "" {
		return nil, fmt.Errorf("%w: tenant_id, media_id, role required", ErrInvalidInput)
	}
	m, err := s.Repo.GetMedia(ctx, p.TenantID, mediaID)
	if err != nil {
		return nil, err
	}
	if err := authorizeRead(p, *m); err != nil {
		return nil, err
	}
	if m.Lifecycle == media.LifecycleDeleted {
		return nil, fmt.Errorf("%w: parent media deleted", ErrPreconditionFailed)
	}
	opts := FindByRoleOpts{AcceptFallback: role == media.AssetRoleDownload}
	a, err := s.Repo.FindByRole(ctx, p.TenantID, mediaID, role, opts)
	if err != nil {
		return nil, err
	}
	if a.StorageKey == "" {
		return nil, fmt.Errorf("%w: asset missing storage_key", ErrPreconditionFailed)
	}
	url, err := s.Storage.PresignGet(ctx, a.StorageKey, ttl)
	if err != nil {
		return nil, err
	}
	return &RoleURL{
		AssetID:     a.ID,
		URL:         url,
		ExpiresAt:   s.Now().Add(ttl),
		ContentType: a.ContentType,
		SizeBytes:   a.SizeBytes,
	}, nil
}
