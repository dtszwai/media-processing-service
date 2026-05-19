package generation

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// ResourceLease represents a single in-flight provider operation slot bound
// to a (resource_class, lease_id). Released via Release.
type ResourceLease struct {
	ID            string
	ResourceClass generation.ResourceClass
	TenantID      string
	JobID         string
	ExpiresAt     time.Time
}

// LeaseRequest is the input to the lease acquisition primitives.
type LeaseRequest struct {
	ResourceClass generation.ResourceClass
	TenantID      string
	JobID         string
	TTL           time.Duration
}

// ResourceLessor abstracts AcquireResource.
type ResourceLessor interface {
	AcquireResource(ctx context.Context, req LeaseRequest) (*ResourceLease, error)
	ReleaseResource(ctx context.Context, lease *ResourceLease) error
}

// LeaseScopedRunner wraps a provider call in acquire/release. Nil lessor
// short-circuits to a no-op so tests don't need a real one.
type LeaseScopedRunner struct {
	Lessor ResourceLessor
}

func NewLeaseScopedRunner(l ResourceLessor) *LeaseScopedRunner {
	return &LeaseScopedRunner{Lessor: l}
}

// WithLease acquires a lease, runs fn, and releases. Acquire failure surfaces
// the underlying transient error so SQS retries with backoff.
func (r *LeaseScopedRunner) WithLease(ctx context.Context, req LeaseRequest, fn func(ctx context.Context, lease *ResourceLease) error) error {
	if r == nil || r.Lessor == nil {
		return fn(ctx, nil)
	}
	lease, err := r.Lessor.AcquireResource(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = r.Lessor.ReleaseResource(ctx, lease) }()
	return fn(ctx, lease)
}
