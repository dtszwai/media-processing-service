package generation_test

import (
	"context"
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestMemResourceLessor_HonorsCap(t *testing.T) {
	l := gen.NewMemResourceLessor(map[generation.ResourceClass]int{generation.ResourceProvider: 2})
	ctx := context.Background()
	a, err := l.AcquireResource(ctx, gen.LeaseRequest{ResourceClass: generation.ResourceProvider})
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	b, err := l.AcquireResource(ctx, gen.LeaseRequest{ResourceClass: generation.ResourceProvider})
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if _, err := l.AcquireResource(ctx, gen.LeaseRequest{ResourceClass: generation.ResourceProvider}); err == nil {
		t.Fatalf("third acquire should have failed: cap=2")
	}
	if err := l.ReleaseResource(ctx, a); err != nil {
		t.Fatalf("release a: %v", err)
	}
	if _, err := l.AcquireResource(ctx, gen.LeaseRequest{ResourceClass: generation.ResourceProvider}); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := l.ReleaseResource(ctx, b); err != nil {
		t.Fatalf("release b: %v", err)
	}
}
