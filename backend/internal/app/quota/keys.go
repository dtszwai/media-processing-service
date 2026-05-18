// Package quota implements the generic Reservoir primitive: atomic capacity
// reservation keyed by (scope_type, scope_id, metric, period). One package
// holds tenant cost, API-key cost, vendor cost, service-global cost, request
// counts, storage bytes, and generated-output budgets — only the call sites
// differ, the storage shape and reserve/commit/release semantics are shared.
//
// `available` on the aggregate row is denormalized as `cap - reserved -
// committed + released` so the hot reserve path is a single conditional
// update; DynamoDB conditions cannot do arithmetic, which makes the
// denormalization load-bearing rather than a perf optimization.
package quota

import (
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// AggSK is the fixed sort key on the per-reservoir aggregate row. One AGG
// per (scope_type, scope_id, metric, period) tuple — co-located with the
// per-reservation LEDGER rows so a reservation update + aggregate update
// hit the same partition.
const AggSK = "AGG"

// ReservoirPK returns the partition key for a Reservoir aggregate row.
// Format: RESERVOIR#<scope_type>#<scope_id>#<metric>#<period>. Period is
// caller-controlled (yyyyMMdd for daily, yyyyMM for monthly) so the same
// primitive serves daily cost caps and monthly storage caps without
// introducing a per-period schema.
func ReservoirPK(scope quota.ScopeType, scopeID string, metric quota.Metric, period string) string {
	return "RESERVOIR#" + string(scope) + "#" + scopeID + "#" + string(metric) + "#" + period
}

// LedgerSK returns the sort key for a per-reservation ledger row. The row
// shares the Reservoir's partition so per-reservation lifecycle updates
// co-locate with the aggregate update.
func LedgerSK(reservationID string) string { return "LEDGER#" + reservationID }

// AggKey returns the kv.Key for the aggregate Reservoir row.
func AggKey(scope quota.ScopeType, scopeID string, metric quota.Metric, period string) kv.Key {
	return kv.Key{PK: ReservoirPK(scope, scopeID, metric, period), SK: AggSK}
}

// LedgerKey returns the kv.Key for a per-reservation ledger row.
func LedgerKey(scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string) kv.Key {
	return kv.Key{PK: ReservoirPK(scope, scopeID, metric, period), SK: LedgerSK(reservationID)}
}
