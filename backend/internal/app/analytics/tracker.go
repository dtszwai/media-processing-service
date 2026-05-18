// Package analytics — tracker emits view/download/result-fetch events and
// writes sharded counters + indices on the consumer side.
//
// Two layers cooperate:
//
//   - The Tracker port is what transport handlers call. It accepts a
//     semantic Event and forwards an envelope onto the analytics topic. The
//     port is intentionally narrow: handlers never touch SNS directly.
//   - The Sink consumes those envelopes and applies counter increments.
//     Idempotency is enforced via a ledger row keyed on Event.DedupeKey
//     using attribute_not_exists — a re-publish of the same logical event
//     short-circuits to a no-op without double-counting.
//
// Write protocol:
//
//	TransactWrite gating the counter on the event ledger:
//	  - Put <KIND>_EVT#<day>#<shardK>, SK=EVT#<dedupe_key> with
//	    attribute_not_exists. Duplicate delivery cancels the txn; the counter
//	    increment is skipped while the idempotent index upserts still run.
//	  - Update VIEW#<tid>#<mediaId>#<shardN>, SK=DAY#<day> with ADD count :1.
//
//	Separate writes after the transaction (each individually idempotent):
//	  - Update ANALYTICS_ACTIVE_TENANTS#<day>, SK=TENANT#<tid> with
//	    SET first_seen_at = if_not_exists(first_seen_at, :ts).
//	  - Update CANDIDATE#<tid>#<day>, SK=MEDIA#<mediaId> same shape.
//
// Doing active-tenant + candidate writes INSIDE the same transaction would
// cause the SECOND event for the same (tenant, day) to cancel the entire
// txn → undercount. They run as separate Updates outside the transaction so
// duplicates short-circuit to no-op without affecting the counter increment.
package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

// EventType is the producer-side taxonomy. Each value maps to one EventKind
// (VIEW vs DOWNLOAD) for counter routing — the consumer's PK prefix depends on
// the underlying kind, not the surface type, so multiple types can roll up into
// the same counter family.
type EventType string

const (
	EventTypeMediaView             EventType = "MEDIA_VIEW"
	EventTypeMediaDownload         EventType = "MEDIA_DOWNLOAD"
	EventTypeAssetPresign          EventType = "ASSET_PRESIGN"
	EventTypeGenerationResultFetch EventType = "GENERATION_RESULT_FETCH"
	EventTypeWebhookDelivered      EventType = "WEBHOOK_DELIVERED"
	EventTypeSafetyBlock           EventType = "SAFETY_BLOCK"
	EventTypeSafetyGateFail        EventType = "SAFETY_GATE_FAIL"
)

// EventKind is the consumer-side classifier; the ledger and counter PKs are
// prefixed with the kind so reads can sum without touching the surface type.
type EventKind string

const (
	EventView     EventKind = "VIEW"
	EventDownload EventKind = "DOWNLOAD"
)

// SchemaName is the envelope identifier published as an SNS message
// attribute. Consumers filter on this so an unrelated topic subscriber can't
// accidentally process analytics envelopes.
const SchemaName = "analytics.event.recorded.v1"

// ViewShards is the counter row partition fan-out.
const ViewShards = 16

// LedgerShards is the event ledger fan-out, independent from ViewShards.
const LedgerShards = 16

// LedgerTTL — 14 days matches SQS retention; shorter would let post-retention
// redeliveries double-count.
const LedgerTTL = 14 * 24 * time.Hour

// Event is the wire envelope published to SNS and re-consumed by the worker.
//
// AnalyticsEventID is a per-publish trace id; correctness does not depend on
// it. DedupeKey is the idempotency primitive — the consumer ledger uses
// DedupeKey as its SK so two publishes of the same logical event collapse
// to one counter increment, even across SQS redelivery.
type Event struct {
	AnalyticsEventID string    `json:"analytics_event_id"`
	EventType        EventType `json:"event_type"`
	DedupeKey        string    `json:"dedupe_key"`
	TenantID         string    `json:"tenant_id"`
	MediaID          string    `json:"media_id"`
	AssetID          string    `json:"asset_id,omitempty"`
	PrincipalID      string    `json:"principal_id,omitempty"`
	Format           string    `json:"format,omitempty"`
	Source           string    `json:"source,omitempty"`
	OccurredAt       time.Time `json:"occurred_at"`
}

// Kind returns the consumer-side VIEW vs DOWNLOAD classifier the sink uses
// to pick the counter family. Centralised here so producers never carry the
// mapping themselves.
func (e Event) Kind() EventKind {
	switch e.EventType {
	case EventTypeMediaDownload:
		return EventDownload
	default:
		// View-shaped surface types (MEDIA_VIEW, ASSET_PRESIGN,
		// GENERATION_RESULT_FETCH) all roll into the VIEW counter family.
		// Webhook + safety types are still tagged kind=VIEW so they go
		// through the same ledger; reads filter on event_type.
		return EventView
	}
}

// ComputeDedupeKey returns the sha256 over a stable tuple. The hour bucket
// makes a fast user retry within the same hour collapse to one event while
// a legitimate retry next hour still counts.
//
// Choice rationale: tenant + media + asset + principal alone would let a
// long-running session re-emit the same key forever and never increment.
// Folding in the hour bucket caps the dedupe window without leaking
// per-minute jitter into the key shape.
func ComputeDedupeKey(eventType EventType, tenantID, mediaID, assetID, principalID string, occurredAt time.Time) string {
	hourBucket := occurredAt.UTC().Format("2006010215")
	h := sha256.New()
	// Pipe-separated tuple; the individual ids never contain '|' (they are
	// random-id slugs), so the join is unambiguous.
	h.Write([]byte(strings.Join([]string{
		tenantID, mediaID, assetID, principalID, string(eventType), hourBucket,
	}, "|")))
	return hex.EncodeToString(h.Sum(nil))
}

// Tracker is the transport-facing port. Implementations decide whether the
// emission goes via SNS, an in-process queue, or a test fake. Track must
// never bubble errors back up to user-facing responses — see the wrapper at
// the bootstrap layer that logs + swallows.
type Tracker interface {
	Track(ctx context.Context, evt Event) error
}

// Publisher is the underlying transport the production Tracker writes to.
// Kept narrower than eventbus.Publisher so a test fake doesn't have to
// implement attribute encoding.
type Publisher interface {
	PublishAnalyticsEvent(ctx context.Context, evt Event) error
}

// SNSTracker bridges Track onto a Publisher. Tests substitute the Publisher
// (or, more commonly, swap the whole Tracker interface).
type SNSTracker struct {
	Pub Publisher
	Now func() time.Time
}

// NewTracker constructs a Tracker bound to a Publisher.
func NewTracker(pub Publisher) *SNSTracker {
	return &SNSTracker{Pub: pub, Now: func() time.Time { return time.Now().UTC() }}
}

// Track fills missing envelope fields and forwards onto the Publisher.
// AnalyticsEventID and DedupeKey are computed if the caller omitted them,
// so handler sites can fire a one-liner with just the semantic fields.
func (t *SNSTracker) Track(ctx context.Context, evt Event) error {
	if t == nil || t.Pub == nil {
		return errors.New("analytics: no publisher")
	}
	if evt.EventType == "" {
		return errors.New("analytics: event_type required")
	}
	if evt.TenantID == "" {
		return errors.New("analytics: tenant_id required")
	}
	if evt.OccurredAt.IsZero() {
		now := time.Now
		if t.Now != nil {
			now = t.Now
		}
		evt.OccurredAt = now().UTC()
	}
	if evt.AnalyticsEventID == "" {
		evt.AnalyticsEventID = "ae_" + randid.New()
	}
	if evt.DedupeKey == "" {
		evt.DedupeKey = ComputeDedupeKey(evt.EventType, evt.TenantID, evt.MediaID, evt.AssetID, evt.PrincipalID, evt.OccurredAt)
	}
	return t.Pub.PublishAnalyticsEvent(ctx, evt)
}

// eventbusPublisher wraps an eventbus.Publisher into the analytics.Publisher
// port. It marshals the Event envelope to JSON and tags the schema name +
// event type as SNS message attributes so a future fan-out filter policy
// can route on either dimension.
type eventbusPublisher struct {
	pub eventbus.Publisher
}

// NewBusPublisher binds an analytics.Publisher to a pre-bound eventbus topic.
// Returns nil when pub is nil; the tracker treats a nil publisher as "no
// analytics wired" and short-circuits Track to an error the caller logs.
func NewBusPublisher(pub eventbus.Publisher) Publisher {
	if pub == nil {
		return nil
	}
	return &eventbusPublisher{pub: pub}
}

// PublishAnalyticsEvent marshals the Event and forwards to the bound topic.
func (p *eventbusPublisher) PublishAnalyticsEvent(ctx context.Context, evt Event) error {
	body, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	attrs := map[string]string{
		"schema":     SchemaName,
		"event_type": string(evt.EventType),
	}
	_, perr := p.pub.Publish(ctx, body, attrs)
	return perr
}

// NoopTracker is the safe default when no analytics topic is wired (e.g.
// minimal local bring-up). Track returns nil so handlers never see errors
// they would have to swallow themselves.
type NoopTracker struct{}

// Track on NoopTracker is a deliberate no-op.
func (NoopTracker) Track(context.Context, Event) error { return nil }

// Sink writes a single Event using the documented write protocol.
type Sink struct {
	KV kv.KV
}

// NewSink binds the sink to a kv driver.
func NewSink(k kv.KV) *Sink { return &Sink{KV: k} }

// Apply runs the txn-then-upserts sequence. Idempotent: a duplicate Event
// (same DedupeKey) short-circuits via the event-ledger conditional check.
func (s *Sink) Apply(ctx context.Context, evt Event) error {
	if evt.DedupeKey == "" || evt.TenantID == "" || evt.MediaID == "" {
		return errors.New("analytics.Sink: dedupe_key, tenant_id, media_id required")
	}
	day := evt.OccurredAt.UTC().Format("20060102")
	ledgerShard := shardkey.Of(evt.DedupeKey, LedgerShards)
	counterShard := shardkey.Of(evt.TenantID+"#"+evt.MediaID, ViewShards)
	expires := evt.OccurredAt.Add(LedgerTTL).Unix()
	now := evt.OccurredAt.Format(time.RFC3339Nano)

	kind := evt.Kind()
	prefix := "VIEW"
	if kind == EventDownload {
		prefix = "DOWNLOAD"
	}

	ledger := map[string]any{
		"PK":         prefix + "_EVT#" + day + "#" + strconv.Itoa(ledgerShard),
		"SK":         "EVT#" + evt.DedupeKey,
		"tenant_id":  evt.TenantID,
		"media_id":   evt.MediaID,
		"kind":       string(kind),
		"event_type": string(evt.EventType),
		"expires_at": expires,
		"created_at": now,
	}
	if evt.AnalyticsEventID != "" {
		ledger["analytics_event_id"] = evt.AnalyticsEventID
	}
	if evt.AssetID != "" {
		ledger["asset_id"] = evt.AssetID
	}
	if evt.Format != "" {
		ledger["format"] = evt.Format
	}
	if evt.PrincipalID != "" {
		ledger["principal_id"] = evt.PrincipalID
	}

	counterKey := kv.Key{
		PK: prefix + "#" + evt.TenantID + "#" + evt.MediaID + "#" + strconv.Itoa(counterShard),
		SK: "DAY#" + day,
	}

	err := s.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                ledger,
			ConditionExpression: "attribute_not_exists(SK)",
		}},
		{Update: &kv.UpdateOp{
			Key:              counterKey,
			UpdateExpression: "ADD #c :one",
			ExpressionAttributeNames: kv.Names{
				"#c": "count",
			},
			ExpressionAttributeValues: kv.Values{
				":one": 1,
			},
		}},
	})
	if err != nil {
		if !isDuplicateLedgerCancel(err) {
			return err
		}
	}

	if ierr := s.KV.Update(ctx, kv.UpdateOp{
		Key:              kv.Key{PK: "ANALYTICS_ACTIVE_TENANTS#" + day, SK: "TENANT#" + evt.TenantID},
		UpdateExpression: "SET first_seen_at = if_not_exists(first_seen_at, :ts)",
		ExpressionAttributeValues: kv.Values{
			":ts": now,
		},
	}); ierr != nil {
		return ierr
	}
	if ierr := s.KV.Update(ctx, kv.UpdateOp{
		Key:              kv.Key{PK: "CANDIDATE#" + evt.TenantID + "#" + day, SK: "MEDIA#" + evt.MediaID},
		UpdateExpression: "SET first_seen_at = if_not_exists(first_seen_at, :ts)",
		ExpressionAttributeValues: kv.Values{
			":ts": now,
		},
	}); ierr != nil {
		return ierr
	}
	return nil
}

// isDuplicateLedgerCancel returns true when the first txn op (the ledger Put)
// cancelled on a condition failure — i.e. the event was already recorded.
func isDuplicateLedgerCancel(err error) bool {
	var txn kv.TxnError
	if !errors.As(err, &txn) {
		return false
	}
	items := txn.Items()
	if len(items) == 0 {
		return false
	}
	return items[0].ConditionFailed
}

// AsJSON returns the SNS body for an Event.
func (e Event) AsJSON() ([]byte, error) { return json.Marshal(e) }
