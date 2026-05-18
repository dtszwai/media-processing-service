package outbox

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// BuildPutOp returns the kv.PutOp that inserts r as a live outbox row. The
// caller appends it to a TransactWrite alongside the state-change ops so the
// row only appears when the state change commits. Producers must NOT supply
// SNS message attributes here — the relay derives them at publish time under
// a routing policy that validates against the allowed enums.
func BuildPutOp(r Row) kv.PutOp {
	item := baseOutboxItem(r.Stream, PK(r.Stream, r.PartitionTS, r.Shard), SK(r.EventID, r.PartitionTS), r.Body, r.PartitionTS)
	item["event_id"] = r.EventID
	item["event_type"] = r.EventType
	item["tenant_id"] = r.TenantID
	if r.Reason != "" {
		item["reason"] = r.Reason
	}
	return kv.PutOp{
		Item:                item,
		ConditionExpression: "attribute_not_exists(PK)",
	}
}

// JobRow is the per-stage generation outbox shape. Distinct from Row because
// generation rows carry tier + stage + resource_class — the three semantic
// fields the relay's routing policy maps to SNS message attributes for the
// per-tier × resource-class fanout. PartitionTS doubles as next_attempt_at's
// base.
type JobRow struct {
	JobID         string
	TenantID      string
	TenantLane    string
	Tier          string
	Stage         string
	ResourceClass string
	Body          []byte
	PartitionTS   time.Time
}

// JobItem is the per-stage generation outbox row. Co-located with the generic
// outbox layout so its key conventions live in one place. The stream is
// always StreamGeneration; producers don't choose it on a per-row basis.
func JobItem(r JobRow) map[string]any {
	pk := PK(StreamGeneration, r.PartitionTS, Shard(r.JobID, 8))
	sk := r.PartitionTS.UTC().Format(time.RFC3339Nano) + "#" + r.JobID + "#" + r.Stage
	item := baseOutboxItem(StreamGeneration, pk, sk, r.Body, r.PartitionTS)
	item["job_id"] = r.JobID
	item["tenant_id"] = r.TenantID
	item["tenant_lane"] = r.TenantLane
	item["tier"] = r.Tier
	item["stage"] = r.Stage
	item["resource_class"] = r.ResourceClass
	return item
}

// baseOutboxItem returns the stream-invariant attribute set shared by every
// outbox row: identity (PK/SK/stream), the body and its content hash, and
// the relay's polling-state fields (status, attempts, lease, next attempt,
// ttl). Stream-specific fields (event_id, job_id, tier…) are layered on by
// each caller.
func baseOutboxItem(stream, pk, sk string, body []byte, partitionTS time.Time) map[string]any {
	return map[string]any{
		"PK":              pk,
		"SK":              sk,
		"stream":          stream,
		"body":            body,
		"body_sha256":     bodySHA256(body),
		"status":          "PENDING",
		"attempts":        0,
		"lease_until":     0,
		"next_attempt_at": partitionTS.Unix(),
		"ttl_epoch":       partitionTS.Add(7 * 24 * time.Hour).Unix(),
	}
}

func bodySHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
