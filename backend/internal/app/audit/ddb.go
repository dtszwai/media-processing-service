package audit

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// auditTTL keeps audit rows queryable for exactly one regulatory year. The
// reference design fixes this so reconciliation windows match across the
// platform; bumping it requires a deliberate change rather than a per-event
// override.
const auditTTL = 365 * 24 * time.Hour

var auditWriteFailures metric.Int64Counter

func init() {
	auditWriteFailures, _ = otel.GetMeterProvider().Meter(obs.MeterName).Int64Counter(
		"audit.write_failures_total",
		metric.WithDescription("Failed immutable audit-row writes by event type."),
		metric.WithUnit("1"),
	)
}

// DDB is the DynamoDB-backed Recorder.
type DDB struct {
	KV    kv.KV
	Now   func() time.Time
	NewID func() string
}

// NewDDB binds the recorder to a kv driver.
func NewDDB(k kv.KV) *DDB {
	return &DDB{KV: k, Now: func() time.Time { return time.Now().UTC() }, NewID: randid.New}
}

// Record writes the audit row. A row already-present (same PK+SK from a
// stable id) collapses to nil so handler-level retries are transparent.
func (d *DDB) Record(ctx context.Context, ev audit.Event) error {
	row := d.buildRow(ev)
	err := d.KV.Put(ctx, row, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
	})
	if errors.Is(err, kv.ErrConditionFailed) {
		return nil
	}
	if err != nil && auditWriteFailures != nil {
		auditWriteFailures.Add(ctx, 1, metric.WithAttributes(attribute.String("event_type", ev.EventType)))
	}
	return err
}

// buildRow normalizes the event and shapes it into the canonical row map.
// Missing CreatedAt / ID are filled here so callers can ship a partial Event
// and still get the canonical layout — important because most call sites
// don't carry a pre-minted ULID.
func (d *DDB) buildRow(ev audit.Event) map[string]any {
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = d.Now()
	}
	if ev.ID == "" {
		ev.ID = d.NewID()
	}
	return BuildEventRow(ev)
}

// BuildEventRow shapes ev into the canonical DDB row map. Exported so the
// per-feature audit writers (e.g. the disclosure-gate auditor in
// app/generation/ddb) can compose the same shape rather than redefining the
// field set and drifting. Callers must pre-fill CreatedAt and ID; this
// function does not assign them.
func BuildEventRow(ev audit.Event) map[string]any {
	created := ev.CreatedAt.UTC().Format(time.RFC3339Nano)
	row := map[string]any{
		"PK":          AuditPK(ev.TenantID, ev.CreatedAt),
		"SK":          AuditSK(ev.CreatedAt, ev.EventType, ev.Entity.ID, ev.ID),
		"audit_id":    ev.ID,
		"tenant_id":   ev.TenantID,
		"event_type":  ev.EventType,
		"actor_type":  string(ev.ActorType),
		"actor_id":    ev.ActorID,
		"entity_type": ev.Entity.Type,
		"entity_id":   ev.Entity.ID,
		"decision":    string(ev.Decision),
		"reason_code": ev.ReasonCode,
		"created_at":  created,
		"ttl_epoch":   ev.CreatedAt.Add(auditTTL).Unix(),
		// gsi_audit_entity: per-entity history lookup. Composed of the entity
		// type+id so multiple entity types can share one GSI.
		"gsi_audit_entity_pk": EntityGSIPK(ev.Entity.Type, ev.Entity.ID),
		"gsi_audit_entity_sk": GSISK(ev.CreatedAt, ev.ID),
		// gsi_audit_actor: per-actor activity lookup.
		"gsi_audit_actor_pk": ActorGSIPK(string(ev.ActorType), ev.ActorID),
		"gsi_audit_actor_sk": GSISK(ev.CreatedAt, ev.ID),
	}
	if len(ev.Summary) > 0 {
		row["summary"] = ev.Summary
	}
	if ev.BeforeHash != "" {
		row["before_hash"] = ev.BeforeHash
	}
	if ev.AfterHash != "" {
		row["after_hash"] = ev.AfterHash
	}
	if ev.RequestID != "" {
		row["request_id"] = ev.RequestID
	}
	if ev.TraceID != "" {
		row["trace_id"] = ev.TraceID
	}
	return row
}

// AuditPK returns the partition key for an audit row. Keyed by tenant and
// day so per-tenant retention scans (and the operator "audit log for tenant
// X today" query) only touch one partition.
func AuditPK(tenantID string, createdAt time.Time) string {
	return "AUDIT#TENANT#" + tenantID + "#" + createdAt.UTC().Format("20060102")
}

// AuditSK returns the sort key for an audit row. Format keeps it
// monotonically increasing and uniquely identifies the row inside the day
// partition without requiring readers to know the audit_id ahead of time.
func AuditSK(createdAt time.Time, eventType, entityID, auditID string) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "#" + eventType + "#" + entityID + "#" + auditID
}

// EntityGSIPK returns the partition key for gsi_audit_entity. Untyped
// entity refs collapse to "ENTITY##" so missing entity types are still
// queryable rather than rejected at write time.
func EntityGSIPK(entityType, entityID string) string {
	return "ENTITY#" + entityType + "#" + entityID
}

// ActorGSIPK returns the partition key for gsi_audit_actor.
func ActorGSIPK(actorType, actorID string) string {
	return "ACTOR#" + actorType + "#" + actorID
}

// GSISK is the shared range key for both audit GSIs.
func GSISK(createdAt time.Time, auditID string) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "#" + auditID
}
