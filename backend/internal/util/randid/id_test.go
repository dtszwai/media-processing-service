package randid_test

import (
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

func TestNew_Shape(t *testing.T) {
	a := randid.New()
	if len(a) != 32 {
		t.Fatalf("len = %d, want 32", len(a))
	}
	for _, c := range a {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("non-hex char %q in %q", c, a)
		}
	}
}

func TestNew_Unique(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 10_000; i++ {
		v := randid.New()
		if _, dup := seen[v]; dup {
			t.Fatalf("collision after %d draws: %s", i, v)
		}
		seen[v] = struct{}{}
	}
}
