package media_test

import (
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

func TestStorageKey(t *testing.T) {
	cases := []struct {
		tenant, mediaID, assetID, ext, want string
	}{
		{"t1", "m1", "a1", "png", "t1/m1/assets/a1.png"},
		{"t1", "m1", "a1", "", "t1/m1/assets/a1"},
		{"tenant-A", "med-B", "ast-C", "wav", "tenant-A/med-B/assets/ast-C.wav"},
	}
	for _, c := range cases {
		if got := media.StorageKey(c.tenant, c.mediaID, c.assetID, c.ext); got != c.want {
			t.Fatalf("StorageKey(%q,%q,%q,%q) = %q, want %q", c.tenant, c.mediaID, c.assetID, c.ext, got, c.want)
		}
	}
}

func TestLifecycleConstants(t *testing.T) {
	if media.LifecyclePending != "PENDING" {
		t.Fatalf("constant drift")
	}
	if media.LifecycleRunning != "RUNNING" {
		t.Fatalf("constant drift")
	}
	if media.AssetKindOriginal != "ORIGINAL" {
		t.Fatalf("constant drift")
	}
}
