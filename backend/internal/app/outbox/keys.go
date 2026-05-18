// Package outbox owns the transactional-outbox relay for cross-stream
// publication. Producers write rows via BuildPutOp inside their state-change
// transactions; the standalone outbox-relay worker drains and publishes via
// eventbus, deriving SNS message attributes from semantic row fields under a
// RoutingPolicy.
package outbox

import (
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

// Stream names for the live outbox layout. Producers and the relay must agree.
const (
	StreamMedia        = "MEDIA"
	StreamMediaCleanup = "MEDIA_CLEANUP"
	StreamGeneration   = "GEN"
)

// AllStreams enumerates every stream the relay drains. Single source of truth
// for the per-stream worker fan-out, tests, and ops tooling.
var AllStreams = []string{StreamMedia, StreamMediaCleanup, StreamGeneration}

// DayLayout formats a UTC date as YYYYMMDD — used in DLQ partition keys.
const DayLayout = "20060102"

// HourLayout formats a UTC timestamp as YYYYMMDDHH — bucketed live partitions.
const HourLayout = "2006010215"

// Shard returns sha256(s) mod n.
func Shard(s string, n int) int { return shardkey.Of(s, n) }

// PK returns the partition key for a live outbox row.
// Hour-bucketed + sharded so a single "OUTBOX" PK never hot-partitions.
func PK(stream string, ts time.Time, shard int) string {
	return fmt.Sprintf("OUTBOX#%s#%s#%d", stream, ts.UTC().Format(HourLayout), shard)
}

// SK returns the sort key for a live outbox row.
func SK(eventID string, ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano) + "#" + eventID
}

// CheckpointPK partitions the per-shard hour-completion checkpoint.
func CheckpointPK(stream string, shard int) string {
	return fmt.Sprintf("OUTBOX_CHECKPOINT#%s#%d", stream, shard)
}

// CheckpointSK is the fixed SK on checkpoint rows.
const CheckpointSK = "CHECKPOINT"

// DLQPK partitions failed rows by day for poison cleanup + audit.
func DLQPK(stream string, ts time.Time) string {
	return fmt.Sprintf("OUTBOX_DLQ#%s#%s", stream, ts.UTC().Format(DayLayout))
}

// Row is one transactional-outbox row for the media streams. Producers fill
// the semantic fields and call BuildPutOp; the relay derives SNS message
// attributes under a RoutingPolicy on read. EventType + TenantID are the only
// fields the policy needs for media routing.
type Row struct {
	Stream      string // "MEDIA" | "MEDIA_CLEANUP"
	PartitionTS time.Time
	Shard       int
	EventID     string
	EventType   string
	TenantID    string
	// Reason annotates cleanup rows with a human-readable cause (e.g.
	// "SIZE_CAP_EXCEEDED", "STALE_PENDING"). Persisted alongside Body for
	// operator visibility but never feeds the routing policy.
	Reason string
	Body   []byte
}
