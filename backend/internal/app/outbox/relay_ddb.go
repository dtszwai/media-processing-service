package outbox

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

// Relay drains a sharded, hour-bucketed outbox stream. Each row is leased
// with a 60s conditional UpdateItem, published via the bound Publisher, then
// deleted. On failure the lease releases with backoff; after MaxAttempts the
// row migrates to OUTBOX_DLQ#<stream>#<day>.
//
// SNS message attributes are derived from the persisted semantic fields by
// the RoutingPolicy, not supplied by producers. One typo in a hand-rolled
// attribute map silently drops every matching message behind the topic's
// filter policy with no metric to alert on; centralising derivation in the
// relay turns that silent failure mode into an explicit DLQ entry.
type Relay struct {
	KV          kv.KV
	Pub         eventbus.Publisher
	Stream      string
	Policy      RoutingPolicy
	Shards      int
	SkewWindow  time.Duration
	LeaseTTL    time.Duration
	MaxAttempts int
	Now         func() time.Time
	// Instruments tracks per-row publish outcomes + latency. Nil is
	// permissive — NewRelay populates it with obs.Noop() so call sites and
	// tests don't have to nil-check before Add/Record.
	Instruments *obs.Instruments
}

// NewRelay binds a Relay to the given stream + publisher with sensible
// defaults. The default routing policy is the static enum-validated one;
// callers can replace it by setting Relay.Policy after construction.
func NewRelay(k kv.KV, stream string, shards int, pub eventbus.Publisher) *Relay {
	return &Relay{
		KV: k, Pub: pub, Stream: stream, Shards: shards,
		Policy:      DefaultPolicy{},
		SkewWindow:  5 * time.Minute,
		LeaseTTL:    60 * time.Second,
		MaxAttempts: 5,
		Now:         func() time.Time { return time.Now().UTC() },
		Instruments: obs.Noop(),
	}
}

// WithInstruments swaps the noop-default instrument bundle for a real one. The
// builder shape keeps NewRelay's signature stable when the call-site already
// owns a process-wide *Instruments.
func (r *Relay) WithInstruments(i *obs.Instruments) *Relay {
	if i != nil {
		r.Instruments = i
	}
	return r
}

// Step drains one batch from one shard.
func (r *Relay) Step(ctx context.Context, shard int) (int, error) {
	checkpoint, err := r.loadCheckpoint(ctx, shard)
	if err != nil {
		return 0, err
	}
	now := r.Now()
	lastBucket := now.Truncate(time.Hour)
	processed := 0
	for bucket := checkpoint; !bucket.After(lastBucket); bucket = bucket.Add(time.Hour) {
		pk := PK(r.Stream, bucket, shard)
		page, err := r.KV.Query(ctx, kv.QueryRequest{
			KeyConditionExpression: "PK = :pk",
			ExpressionAttributeValues: kv.Values{
				":pk": pk,
			},
			Limit: 25,
		})
		if err != nil {
			return processed, err
		}
		empty := true
		for _, row := range page.Items {
			empty = false
			if err := r.drainOne(ctx, pk, row); err != nil {
				return processed, err
			}
			processed++
		}
		if empty && !bucket.Add(time.Hour).Add(r.SkewWindow).After(now) {
			if err := r.saveCheckpoint(ctx, shard, bucket.Add(time.Hour)); err != nil {
				return processed, err
			}
		}
	}
	return processed, nil
}

func (r *Relay) drainOne(ctx context.Context, pk string, row kv.Row) error {
	sk, _ := row.Get("SK").(string)
	if sk == "" {
		return errors.New("outbox: missing SK")
	}
	attempts := readN(row, "attempts")
	now := r.Now()
	r.emitPendingAge(ctx, sk, now)
	leaseExpiry := now.Add(r.LeaseTTL).Unix()

	if err := r.KV.Update(ctx, kv.UpdateOp{
		Key:                 kv.Key{PK: pk, SK: sk},
		ConditionExpression: "(attribute_not_exists(lease_until) OR lease_until < :now) AND (attribute_not_exists(next_attempt_at) OR next_attempt_at <= :now)",
		UpdateExpression:    "SET lease_until = :exp, attempts = if_not_exists(attempts, :zero) + :one",
		ExpressionAttributeValues: kv.Values{
			":now":  now.Unix(),
			":exp":  leaseExpiry,
			":zero": 0,
			":one":  1,
		},
	}); err != nil {
		if errors.Is(err, kv.ErrConditionFailed) {
			return nil
		}
		return err
	}

	eventType := readS(row, "event_type")
	attrs, perr := r.Policy.AttributesFor(pendingFromRow(r.Stream, row))
	if perr != nil {
		r.emitPublished(ctx, eventType, obs.OutboxResultPoisoned, 0)
		return r.movePoison(ctx, pk, sk, row, "routing_policy_failed: "+perr.Error())
	}

	body := bodyBytes(row)
	pubStart := r.Now()
	_, pubErr := r.Pub.Publish(ctx, body, attrs)
	pubElapsedMs := float64(r.Now().Sub(pubStart)) / float64(time.Millisecond)
	if pubErr != nil {
		if attempts+1 >= int64(r.MaxAttempts) {
			r.emitPublished(ctx, eventType, obs.OutboxResultPoisoned, pubElapsedMs)
			return r.movePoison(ctx, pk, sk, row, "max_attempts_exceeded: "+pubErr.Error())
		}
		r.emitPublished(ctx, eventType, obs.OutboxResultFailed, pubElapsedMs)
		_ = r.KV.Update(ctx, kv.UpdateOp{
			Key:              kv.Key{PK: pk, SK: sk},
			UpdateExpression: "SET lease_until = :zero, next_attempt_at = :next",
			ExpressionAttributeValues: kv.Values{
				":zero": 0,
				":next": now.Add(30 * time.Second).Unix(),
			},
		})
		return nil
	}
	r.emitPublished(ctx, eventType, obs.OutboxResultPublished, pubElapsedMs)
	return r.KV.Delete(ctx, kv.DeleteOp{Key: kv.Key{PK: pk, SK: sk}})
}

func (r *Relay) emitPendingAge(ctx context.Context, sk string, now time.Time) {
	prefix, _, _ := strings.Cut(sk, "#")
	createdAt, err := time.Parse(time.RFC3339Nano, prefix)
	if err != nil {
		return
	}
	age := now.Sub(createdAt)
	if age < 0 {
		return
	}
	r.Instruments.OutboxPendingAge.Record(ctx, age.Seconds())
}

// emitPublished increments outbox.published_total and records
// outbox.relay_latency_ms for one row. eventType labels the per-stream
// event class so dashboards can break down published rates by event without
// pulling tenant id into the label space.
func (r *Relay) emitPublished(ctx context.Context, eventType, result string, elapsedMs float64) {
	if eventType == "" {
		eventType = "unspecified"
	}
	r.Instruments.OutboxPublished.Add(ctx, 1, metric.WithAttributes(
		attribute.String("stream", r.Stream),
		attribute.String("event_type", eventType),
		attribute.String("result", result),
	))
	if elapsedMs > 0 {
		r.Instruments.OutboxRelayLatency.Record(ctx, elapsedMs, metric.WithAttributes(
			attribute.String("stream", r.Stream),
		))
	}
}

func (r *Relay) movePoison(ctx context.Context, pk, sk string, row kv.Row, reason string) error {
	now := r.Now()
	var raw map[string]any
	if err := row.Unmarshal(&raw); err != nil {
		return err
	}
	raw["PK"] = DLQPK(r.Stream, now)
	raw["SK"] = sk
	raw["status"] = "FAILED"
	raw["failure_reason"] = reason
	raw["moved_at"] = now.Format(time.RFC3339Nano)
	raw["ttl_epoch"] = now.Add(30 * 24 * time.Hour).Unix()
	delete(raw, "expires_at")

	// attribute_not_exists makes the DLQ write idempotent across retries and
	// preserves the AGENTS.md invariant that audit-shaped rows are immutable.
	return r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                raw,
			ConditionExpression: "attribute_not_exists(PK)",
		}},
		{Delete: &kv.DeleteOp{Key: kv.Key{PK: pk, SK: sk}}},
	})
}

func (r *Relay) loadCheckpoint(ctx context.Context, shard int) (time.Time, error) {
	var row checkpointRow
	err := r.KV.Get(ctx, kv.Key{PK: CheckpointPK(r.Stream, shard), SK: CheckpointSK}, &row)
	if errors.Is(err, kv.ErrNotFound) {
		return r.Now().Add(-time.Hour).Truncate(time.Hour), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if row.LastCompletedHour == "" {
		return r.Now().Add(-time.Hour).Truncate(time.Hour), nil
	}
	t, err := time.Parse(HourLayout, row.LastCompletedHour)
	if err != nil {
		return r.Now().Add(-time.Hour).Truncate(time.Hour), nil
	}
	return t.Add(time.Hour), nil
}

func (r *Relay) saveCheckpoint(ctx context.Context, shard int, completedThrough time.Time) error {
	last := completedThrough.Add(-time.Hour).Format(HourLayout)
	return r.KV.Update(ctx, kv.UpdateOp{
		Key:              kv.Key{PK: CheckpointPK(r.Stream, shard), SK: CheckpointSK},
		UpdateExpression: "SET last_completed_hour = :h",
		ExpressionAttributeValues: kv.Values{
			":h": last,
		},
	})
}

type checkpointRow struct {
	LastCompletedHour string `dynamodbav:"last_completed_hour"`
}

func readN(row kv.Row, k string) int64 {
	switch v := row.Get(k).(type) {
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func readS(row kv.Row, k string) string {
	s, _ := row.Get(k).(string)
	return s
}

func pendingFromRow(stream string, row kv.Row) PendingRow {
	return PendingRow{
		Stream:        stream,
		EventType:     readS(row, "event_type"),
		TenantID:      readS(row, "tenant_id"),
		TenantLane:    readS(row, "tenant_lane"),
		Tier:          readS(row, "tier"),
		Stage:         readS(row, "stage"),
		ResourceClass: readS(row, "resource_class"),
	}
}

func bodyBytes(row kv.Row) []byte {
	switch v := row.Get("body").(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	return nil
}
