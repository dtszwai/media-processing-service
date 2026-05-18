package media_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

// seedThreeRoles plants ORIGINAL + PREVIEW + THUMBNAIL assets for a single
// media. Each asset is COMPLETE so the role-selector queries see real targets.
// Returns the asset ids in the order seeded.
func seedThreeRoles(t *testing.T, repo *memRepo, tenantID, mediaID string) (origID, previewID, thumbID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := repo.PutMedia(context.Background(), media.Media{
		ID: mediaID, TenantID: tenantID, Type: media.TypeImage,
		Lifecycle: media.LifecycleComplete, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("PutMedia: %v", err)
	}
	origID = "ast_orig"
	previewID = "ast_preview"
	thumbID = "ast_thumb"
	for _, a := range []media.Asset{
		{ID: origID, MediaID: mediaID, TenantID: tenantID, Kind: media.AssetKindOriginal, Role: media.AssetRoleOriginal, Lifecycle: media.AssetLifecycleComplete, StorageKey: "k/orig", ContentType: "image/png", SizeBytes: 100, CreatedAt: now, UpdatedAt: now},
		{ID: previewID, MediaID: mediaID, TenantID: tenantID, Kind: media.AssetKindDerived, Role: media.AssetRolePreview, Lifecycle: media.AssetLifecycleComplete, StorageKey: "k/preview", ContentType: "image/png", SizeBytes: 50, CreatedAt: now, UpdatedAt: now},
		{ID: thumbID, MediaID: mediaID, TenantID: tenantID, Kind: media.AssetKindDerived, Role: media.AssetRoleThumbnail, Lifecycle: media.AssetLifecycleComplete, StorageKey: "k/thumb", ContentType: "image/png", SizeBytes: 20, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.PutAsset(context.Background(), a); err != nil {
			t.Fatalf("PutAsset %s: %v", a.ID, err)
		}
	}
	return
}

// TestFindByRole_ExactMatchWinsByPriority verifies that asking for PREVIEW
// returns the PREVIEW asset directly. The priority ordering only matters when
// the requested role partition is empty and AcceptFallback is set; an exact
// match must never silently substitute the ORIGINAL even though ORIGINAL has
// lower priority.
func TestFindByRole_ExactMatchWinsByPriority(t *testing.T) {
	repo := newMemRepo()
	_, previewID, thumbID := seedThreeRoles(t, repo, "t1", "m1")

	got, err := repo.FindByRole(context.Background(), "t1", "m1", media.AssetRolePreview, mediaapp.FindByRoleOpts{})
	if err != nil {
		t.Fatalf("FindByRole(PREVIEW): %v", err)
	}
	if got.ID != previewID {
		t.Fatalf("PREVIEW selector returned %s, want %s", got.ID, previewID)
	}

	got, err = repo.FindByRole(context.Background(), "t1", "m1", media.AssetRoleThumbnail, mediaapp.FindByRoleOpts{})
	if err != nil {
		t.Fatalf("FindByRole(THUMBNAIL): %v", err)
	}
	if got.ID != thumbID {
		t.Fatalf("THUMBNAIL selector returned %s, want %s", got.ID, thumbID)
	}
}

// TestFindByRole_DownloadFallsBackToOriginal exercises the AcceptFallback
// path: a media with only ORIGINAL + PREVIEW + THUMBNAIL assets and no
// DOWNLOAD role should fall back to ORIGINAL when the caller opts in. This
// matches the /download-url contract — "give me bytes if anything is
// available" is honest for downloads but would be misleading for preview /
// thumbnail.
func TestFindByRole_DownloadFallsBackToOriginal(t *testing.T) {
	repo := newMemRepo()
	origID, _, _ := seedThreeRoles(t, repo, "t1", "m1")

	got, err := repo.FindByRole(context.Background(), "t1", "m1", media.AssetRoleDownload, mediaapp.FindByRoleOpts{AcceptFallback: true})
	if err != nil {
		t.Fatalf("FindByRole(DOWNLOAD, fallback): %v", err)
	}
	if got.ID != origID {
		t.Fatalf("DOWNLOAD fallback returned %s, want ORIGINAL %s", got.ID, origID)
	}
}

// TestFindByRole_StrictMissingRoleReturnsErr locks the honest-contract
// behaviour: without AcceptFallback, asking for a role that doesn't exist
// must surface ErrNoAssetForRole rather than silently returning ORIGINAL.
// Previously /preview-url for an audio file would return the original audio
// bytes, which is the rough edge this slice closes.
func TestFindByRole_StrictMissingRoleReturnsErr(t *testing.T) {
	repo := newMemRepo()
	seedThreeRoles(t, repo, "t1", "m1")

	_, err := repo.FindByRole(context.Background(), "t1", "m1", media.AssetRoleDownload, mediaapp.FindByRoleOpts{})
	if !errors.Is(err, mediaapp.ErrNoAssetForRole) {
		t.Fatalf("expected ErrNoAssetForRole without fallback, got %v", err)
	}
}

// adminPrincipal returns a Principal that satisfies authorizeRead via the
// ADMIN role bypass, so role-selector tests focus on lifecycle and wire
// shape rather than auth.
func adminPrincipal(tenantID string) mediaapp.Principal {
	return mediaapp.Principal{TenantID: tenantID, UserID: "u-admin", Roles: []string{"ADMIN"}}
}

// TestGetRoleURLVisible_RejectsDeletedMedia: a soft-deleted parent must
// reject role-keyed presigning even when an asset row still exists.
// Otherwise tombstoned content remains reachable through the role selector.
func TestGetRoleURLVisible_RejectsDeletedMedia(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	now := time.Now().UTC()
	_ = repo.PutMedia(context.Background(), media.Media{
		ID: "m1", TenantID: "t1", Lifecycle: media.LifecycleDeleted, CreatedAt: now, UpdatedAt: now,
	})
	_ = repo.PutAsset(context.Background(), media.Asset{
		ID: "ast_x", MediaID: "m1", TenantID: "t1", Kind: media.AssetKindOriginal,
		Role: media.AssetRoleOriginal, Lifecycle: media.AssetLifecycleComplete, StorageKey: "k/x",
	})

	_, err := svc.GetRoleURLVisible(context.Background(), adminPrincipal("t1"), "m1", media.AssetRoleDownload, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("expected deleted-media rejection, got %v", err)
	}
}

// TestGetRoleURLVisible_HappyPath asserts the wire shape: AssetID + URL +
// content-type + size are all populated from the selected asset. This is
// the contract the transport handler relies on for its success response.
func TestGetRoleURLVisible_HappyPath(t *testing.T) {
	repo := newMemRepo()
	svc := mediaapp.NewService(repo, newMemStorage())
	_, previewID, _ := seedThreeRoles(t, repo, "t1", "m1")

	out, err := svc.GetRoleURLVisible(context.Background(), adminPrincipal("t1"), "m1", media.AssetRolePreview, time.Minute)
	if err != nil {
		t.Fatalf("GetRoleURLVisible: %v", err)
	}
	if out.AssetID != previewID {
		t.Fatalf("AssetID = %s, want %s", out.AssetID, previewID)
	}
	if !strings.HasPrefix(out.URL, "memstorage://") {
		t.Fatalf("URL = %q, want memstorage:// prefix", out.URL)
	}
	if out.ContentType != "image/png" {
		t.Fatalf("ContentType = %q, want image/png", out.ContentType)
	}
	if out.SizeBytes != 50 {
		t.Fatalf("SizeBytes = %d, want 50", out.SizeBytes)
	}
}
