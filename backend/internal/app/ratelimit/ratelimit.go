// Package ratelimit defines the per-key token-bucket the HTTP middleware
// gates request traffic with. The Store interface lets infra plug in a
// process-local or Redis-backed implementation without the middleware
// caring which.
package ratelimit

import (
	"context"
	"time"
)

type Bucket struct {
	Capacity        int
	RefillPerSecond float64
	TTL             time.Duration
	Now             time.Time
}

type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAfter time.Duration
	RetryAfter time.Duration
}

type Store interface {
	Allow(ctx context.Context, keys []string, bucket Bucket) (Decision, error)
}
