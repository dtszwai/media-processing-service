package media

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	connect "connectrpc.com/connect"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	domainmedia "github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	mediapb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1"
)

func TestGetMediaRoleURLConnectEnforcesOwnerVisibility(t *testing.T) {
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{
		media: domainmedia.Media{
			ID:          "med_1",
			TenantID:    "tenant_1",
			OwnerUserID: "user_1",
			Visibility:  domainmedia.VisibilityOwnerPrivate,
			Lifecycle:   domainmedia.LifecycleComplete,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		asset: domainmedia.Asset{
			ID:          "ast_1",
			MediaID:     "med_1",
			TenantID:    "tenant_1",
			Role:        domainmedia.AssetRoleThumbnail,
			Lifecycle:   domainmedia.AssetLifecycleComplete,
			StorageKey:  "tenant_1/med_1/assets/ast_1.png",
			ContentType: "image/png",
			SizeBytes:   123,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	svc := mediaapp.NewService(repo, fakeStorage{})
	svc.Now = func() time.Time { return now }
	server := NewServer(svc, nil)

	ownerCtx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{TenantID: "tenant_1", UserID: "user_1"})
	resp, err := server.GetMediaRoleURL(ownerCtx, connect.NewRequest(&mediapb.GetMediaRoleURLRequest{
		MediaId: "med_1",
		Role:    mediapb.AssetRole_ASSET_ROLE_THUMBNAIL,
	}))
	if err != nil {
		t.Fatalf("owner role url: %v", err)
	}
	if resp.Msg.GetAssetId() != "ast_1" || resp.Msg.GetUrl() != "https://signed.example/tenant_1/med_1/assets/ast_1.png" {
		t.Fatalf("unexpected response: %#v", resp.Msg)
	}

	otherCtx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{TenantID: "tenant_1", UserID: "user_2"})
	_, err = server.GetMediaRoleURL(otherCtx, connect.NewRequest(&mediapb.GetMediaRoleURLRequest{
		MediaId: "med_1",
		Role:    mediapb.AssetRole_ASSET_ROLE_THUMBNAIL,
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("non-owner code = %v, want permission_denied", connect.CodeOf(err))
	}
}

func TestMutatingMethodsRequireOwnerOrWriteGrant(t *testing.T) {
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{
		media: domainmedia.Media{
			ID:          "med_1",
			TenantID:    "tenant_1",
			OwnerUserID: "user_1",
			Visibility:  domainmedia.VisibilityTenantShared,
			Lifecycle:   domainmedia.LifecycleComplete,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		asset: domainmedia.Asset{
			ID:          "ast_1",
			MediaID:     "med_1",
			TenantID:    "tenant_1",
			Role:        domainmedia.AssetRoleOriginal,
			Lifecycle:   domainmedia.AssetLifecycleComplete,
			StorageKey:  "tenant_1/med_1/assets/ast_1.png",
			ContentType: "image/png",
			SizeBytes:   123,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	svc := mediaapp.NewService(repo, fakeStorage{})
	server := NewServer(svc, nil)
	otherCtx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{TenantID: "tenant_1", UserID: "user_2"})

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "delete",
			call: func() error {
				_, err := server.DeleteMedia(otherCtx, connect.NewRequest(&mediapb.DeleteMediaRequest{MediaId: "med_1"}))
				return err
			},
		},
		{
			name: "create assets",
			call: func() error {
				_, err := server.CreateAssets(otherCtx, connect.NewRequest(&mediapb.CreateAssetsRequest{
					MediaId: "med_1",
					Operations: []mediapb.AssetOperation{
						mediapb.AssetOperation_ASSET_OPERATION_IMAGE_THUMBNAIL,
					},
					IdempotencyKey: "derive-1",
				}))
				return err
			},
		},
		{
			name: "retry asset",
			call: func() error {
				_, err := server.RetryAsset(otherCtx, connect.NewRequest(&mediapb.RetryAssetRequest{MediaId: "med_1", AssetId: "ast_1"}))
				return err
			},
		},
		{
			name: "refresh upload",
			call: func() error {
				_, err := server.RefreshPresignedUpload(otherCtx, connect.NewRequest(&mediapb.RefreshPresignedUploadRequest{MediaId: "med_1"}))
				return err
			},
		},
		{
			name: "complete upload",
			call: func() error {
				_, err := server.CompletePresignedUpload(otherCtx, connect.NewRequest(&mediapb.CompletePresignedUploadRequest{MediaId: "med_1"}))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Fatalf("code = %v, want permission_denied (err=%v)", connect.CodeOf(err), err)
			}
		})
	}
}

type fakeRepo struct {
	media domainmedia.Media
	asset domainmedia.Asset
}

func (f *fakeRepo) PutMedia(context.Context, domainmedia.Media) error { return nil }
func (f *fakeRepo) PutAsset(context.Context, domainmedia.Asset) error { return nil }

func (f *fakeRepo) GetMedia(_ context.Context, tenantID, mediaID string) (*domainmedia.Media, error) {
	if tenantID != f.media.TenantID || mediaID != f.media.ID {
		return nil, errors.New("not found")
	}
	m := f.media
	return &m, nil
}

func (f *fakeRepo) GetAsset(_ context.Context, tenantID, mediaID, assetID string) (*domainmedia.Asset, error) {
	if tenantID != f.asset.TenantID || mediaID != f.asset.MediaID || assetID != f.asset.ID {
		return nil, errors.New("not found")
	}
	a := f.asset
	return &a, nil
}

func (f *fakeRepo) ListAssets(_ context.Context, tenantID, mediaID string) ([]domainmedia.Asset, error) {
	if tenantID != f.asset.TenantID || mediaID != f.asset.MediaID {
		return nil, errors.New("not found")
	}
	return []domainmedia.Asset{f.asset}, nil
}

func (f *fakeRepo) FindByRole(_ context.Context, tenantID, mediaID string, role domainmedia.AssetRole, _ mediaapp.FindByRoleOpts) (*domainmedia.Asset, error) {
	if tenantID != f.asset.TenantID || mediaID != f.asset.MediaID || role != f.asset.Role {
		return nil, mediaapp.ErrNoAssetForRole
	}
	a := f.asset
	return &a, nil
}

func (f *fakeRepo) ListByTenant(context.Context, string, mediaapp.ListOpts) (mediaapp.ListPage, error) {
	return mediaapp.ListPage{Items: []domainmedia.Media{f.media}}, nil
}

func (f *fakeRepo) RetryAsset(context.Context, string, string, string, uint32, mediaapp.OutboxRow, time.Time) (*domainmedia.Asset, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRepo) SoftDeleteMediaAndEnqueue(context.Context, string, string, time.Duration, mediaapp.OutboxRow, time.Time) error {
	return errors.New("not implemented")
}

func (f *fakeRepo) InitPresignedUpload(context.Context, domainmedia.Media, domainmedia.Asset, string, string, time.Duration) (domainmedia.Media, domainmedia.Asset, error) {
	return domainmedia.Media{}, domainmedia.Asset{}, errors.New("not implemented")
}

func (f *fakeRepo) CompletePresignedUpload(context.Context, domainmedia.Asset, string, string, mediaapp.OutboxRow, string, string, time.Duration, time.Time) error {
	return errors.New("not implemented")
}

func (f *fakeRepo) FailPresignedUpload(context.Context, string, string, string, mediaapp.OutboxRow, time.Time) error {
	return errors.New("not implemented")
}

func (f *fakeRepo) PutAssetIfAbsent(context.Context, domainmedia.Asset) (bool, error) {
	return false, errors.New("not implemented")
}

func (f *fakeRepo) CompleteMediaIfProcessing(context.Context, string, string, time.Time) error {
	return errors.New("not implemented")
}

func (f *fakeRepo) PutImageMetadata(context.Context, domainmedia.ImageMetadata) error {
	return errors.New("not implemented")
}

func (f *fakeRepo) MarkAssetDeleted(context.Context, string, string, string, time.Time) error {
	return errors.New("not implemented")
}

type fakeStorage struct{}

func (fakeStorage) Put(context.Context, storage.PutInput) (storage.PutOutput, error) {
	return storage.PutOutput{}, errors.New("not implemented")
}

func (fakeStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (fakeStorage) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func (fakeStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + key, nil
}

func (fakeStorage) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("not implemented")
}

func (fakeStorage) GetObjectAttributes(context.Context, string) (storage.ObjectAttrs, error) {
	return storage.ObjectAttrs{}, errors.New("not implemented")
}
