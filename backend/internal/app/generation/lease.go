package generation

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
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

// MemResourceLessor enforces a per-resource-class concurrency cap in-process.
// Used in tests and the local-only path; production swaps in a DDB-backed
// implementation.
type MemResourceLessor struct {
	mu      sync.Mutex
	caps    map[generation.ResourceClass]int
	current map[generation.ResourceClass]int
	leases  map[string]generation.ResourceClass
	clock   func() time.Time
}

func NewMemResourceLessor(caps map[generation.ResourceClass]int) *MemResourceLessor {
	return &MemResourceLessor{
		caps:    caps,
		current: map[generation.ResourceClass]int{},
		leases:  map[string]generation.ResourceClass{},
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

func (l *MemResourceLessor) AcquireResource(_ context.Context, req LeaseRequest) (*ResourceLease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	max, ok := l.caps[req.ResourceClass]
	if !ok {
		max = 1
	}
	if l.current[req.ResourceClass] >= max {
		return nil, errors.New("RESOURCE_CAPACITY_UNAVAILABLE")
	}
	id := "lease_" + randid.New()
	l.current[req.ResourceClass]++
	l.leases[id] = req.ResourceClass
	ttl := req.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ResourceLease{
		ID:            id,
		ResourceClass: req.ResourceClass,
		TenantID:      req.TenantID,
		JobID:         req.JobID,
		ExpiresAt:     l.clock().Add(ttl),
	}, nil
}

func (l *MemResourceLessor) ReleaseResource(_ context.Context, lease *ResourceLease) error {
	if lease == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	class, ok := l.leases[lease.ID]
	if !ok {
		return nil
	}
	delete(l.leases, lease.ID)
	if l.current[class] > 0 {
		l.current[class]--
	}
	return nil
}
