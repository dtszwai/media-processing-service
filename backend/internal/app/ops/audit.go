package ops

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// AuditEvent is the operator-action payload. Operation names the RPC
// (CancelJob, PurgeQueue, …); Target is the canonical identifier the
// console showed in the confirm dialog (job id, queue name, key).
type AuditEvent struct {
	Operation string
	Target    string
	Details   map[string]string
}

// audit writes one AUDIT#OPS#<id> row alongside any state change a mutation
// performed. Failures are logged but never block the mutation — the audit
// table is a forensic surface, not the source of truth.
//
// The per-action row uses PK=AUDIT#OPS, SK=<RFC3339Nano>#<id> so the
// console's "recent operator actions" timeline is one Query. The standalone
// auditapp.Recorder, when wired, also receives the event so audit
// dashboards built on top of the recorder's GSIs (entity / actor) light up
// without a separate ingestion path.
func (s *Service) audit(ctx context.Context, ev AuditEvent) {
	if s.KV == nil {
		return
	}
	now := s.now()
	id := newAuditID(now)
	summary := map[string]any{}
	for k, v := range ev.Details {
		summary[k] = v
	}
	row := map[string]any{
		"PK":         auditOpsPK(),
		"SK":         now.Format(time.RFC3339Nano) + "#" + id,
		"item_type":  "AUDIT_OPS",
		"event_type": "ops." + ev.Operation,
		"tenant_id":  s.LocalTenantID,
		"actor_id":   s.LocalUserID,
		"actor_type": string(audit.ActorOperator),
		"operation":  ev.Operation,
		"target":     ev.Target,
		"details":    summary,
		"decision":   string(audit.DecisionAllow),
		"created_at": now.Format(time.RFC3339Nano),
		// AGENTS.md "Audit rows are immutable" — 1-year TTL matches the
		// standalone audit subsystem.
		"ttl_epoch": now.Add(365 * 24 * time.Hour).Unix(),
	}
	if err := s.KV.Put(ctx, row, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
	}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "ops audit write failed", "operation", ev.Operation, "err", err)
	}
}

func auditOpsPK() string { return "AUDIT#OPS" }

func newAuditID(now time.Time) string {
	// Operator events are infrequent so a nanosecond-suffixed timestamp is
	// a sufficiently unique id; no cross-process contention to defend.
	return now.Format("20060102150405.000000000")
}
