// Package audit is the standalone audit subsystem. It owns the immutable
// AUDIT# row family that records who did what, when, against which entity,
// across auth, API-key, DLQ-replay, idempotency-reset, webhook secret
// rotation, budget override, and tenant-admin events.
//
// The package is intentionally split from analytics: analytics aggregates
// behaviour for dashboards and is intentionally lossy/late; audit is a
// per-event immutable accountability record with a 1-year retention. Two
// readers, two write paths, two access policies — collapsing them would
// either pollute analytics with unaggregated point events or strip audit
// rows of their immutability/TTL guarantees.
package audit

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
)

// Recorder is the port for emitting audit events. Implementations write the
// row atomically with attribute_not_exists(PK) AND attribute_not_exists(SK)
// so concurrent Record calls collapse to nil rather than failing — the same
// stable id/timestamp tuple is treated as a no-op replay.
//
// Recorders MUST NOT return an error for "row already exists" — callers
// invoke Record from request-handling paths where a duplicate retry must not
// be observably different from a clean write.
type Recorder interface {
	Record(ctx context.Context, ev audit.Event) error
}

// NoopRecorder discards events. Useful for tests and for cmd paths where
// audit wiring isn't yet plumbed; call sites should never gate behaviour on
// the recorder being non-nil so substituting Noop never changes semantics.
type NoopRecorder struct{}

// Record satisfies Recorder by discarding ev.
func (NoopRecorder) Record(context.Context, audit.Event) error { return nil }
