package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

const (
	outboxDLQLookbackDays    = 30
	outboxDLQLookupScanCap   = 1000
	outboxDLQLookupPageLimit = 100
)

var ErrOutboxDLQLookupScanLimit = errors.New("outbox dlq: lookup scan limit exceeded")

type OutboxDLQAdmin struct {
	KV       kv.KV
	Recorder auditapp.Recorder
	Now      func() time.Time
}

type OutboxDLQRow struct {
	Stream        string
	Shard         string
	EventID       string
	EventType     string
	TenantID      string
	Body          []byte
	LastError     string
	Attempts      int32
	PartitionTS   time.Time
	FirstFailedAt time.Time
	LastFailedAt  time.Time
}

func NewOutboxDLQAdmin(k kv.KV, recorder auditapp.Recorder) *OutboxDLQAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &OutboxDLQAdmin{KV: k, Recorder: recorder, Now: func() time.Time { return time.Now().UTC() }}
}

func (a *OutboxDLQAdmin) List(ctx context.Context, stream string, limit int32) ([]OutboxDLQRow, error) {
	if a == nil || a.KV == nil {
		return nil, errors.New("outbox dlq: kv required")
	}
	if stream == "" {
		return nil, errors.New("outbox dlq: stream required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	now := a.Now()
	rows := make([]OutboxDLQRow, 0, limit)
	for i := 0; i < outboxDLQLookbackDays && int32(len(rows)) < limit; i++ {
		day := now.AddDate(0, 0, -i)
		page, err := a.KV.Query(ctx, kv.QueryRequest{
			KeyConditionExpression: "PK = :pk",
			ExpressionAttributeValues: kv.Values{
				":pk": outbox.DLQPK(stream, day),
			},
			Limit: limit - int32(len(rows)),
		})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			row, err := decodeOutboxDLQRow(item)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (a *OutboxDLQAdmin) Replay(ctx context.Context, stream, eventID, reason, tenantID, actorUserID string) (string, string, error) {
	raw, err := a.findRaw(ctx, stream, eventID)
	if err != nil {
		return "", "", err
	}
	body, _ := raw["body"].([]byte)
	if stored, _ := raw["body_sha256"].(string); stored != "" && stored != bodySHA256(body) {
		return "", "", errors.New("outbox dlq: body hash mismatch")
	}
	now := a.Now()
	newEventID := "replay-" + eventID + "-" + randid.New()
	raw["PK"] = outbox.PK(stream, now, shardkey.Of(newEventID, 8))
	raw["SK"] = outbox.SK(newEventID, now)
	raw["event_id"] = newEventID
	raw["status"] = "PENDING"
	raw["attempts"] = 0
	raw["lease_until"] = 0
	raw["next_attempt_at"] = now.Unix()
	raw["ttl_epoch"] = now.Add(7 * 24 * time.Hour).Unix()
	raw["replayed_from_event_id"] = eventID
	raw["replay_reason"] = reason
	delete(raw, "failure_reason")
	delete(raw, "moved_at")
	dlqKey := kv.Key{PK: fmt.Sprint(raw["_dlq_pk"]), SK: fmt.Sprint(raw["_dlq_sk"])}
	delete(raw, "_dlq_pk")
	delete(raw, "_dlq_sk")
	ev := a.auditEvent(auditapp.NewOutboxDLQReplayed(tenantID, actorUserID, "outbox-"+stream, eventID, newEventID, ""))
	if err := a.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{Item: auditapp.BuildEventRow(ev), ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)"}},
		{Put: &kv.PutOp{Item: raw, ConditionExpression: "attribute_not_exists(PK)"}},
		{Delete: &kv.DeleteOp{Key: dlqKey}},
	}); err != nil {
		return "", "", err
	}
	return eventID, newEventID, nil
}

func (a *OutboxDLQAdmin) Abandon(ctx context.Context, stream, eventID, reason, tenantID, actorUserID string) error {
	raw, err := a.findRaw(ctx, stream, eventID)
	if err != nil {
		return err
	}
	key := kv.Key{PK: fmt.Sprint(raw["_dlq_pk"]), SK: fmt.Sprint(raw["_dlq_sk"])}
	now := a.Now().Format(time.RFC3339Nano)
	ev := a.auditEvent(auditapp.NewOutboxDLQAbandoned(tenantID, actorUserID, "outbox-"+stream, eventID, reason, ""))
	return a.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                auditapp.BuildEventRow(ev),
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Update: &kv.UpdateOp{
			Key:              key,
			UpdateExpression: "SET #st = :abandoned, abandoned_at = :now, abandon_reason = :reason",
			ExpressionAttributeNames: kv.Names{
				"#st": "status",
			},
			ExpressionAttributeValues: kv.Values{
				":abandoned": "ABANDONED",
				":now":       now,
				":reason":    reason,
			},
		}},
	})
}

func (a *OutboxDLQAdmin) Delete(ctx context.Context, stream, eventID, reason, tenantID, actorUserID string) error {
	raw, err := a.findRaw(ctx, stream, eventID)
	if err != nil {
		return err
	}
	key := kv.Key{PK: fmt.Sprint(raw["_dlq_pk"]), SK: fmt.Sprint(raw["_dlq_sk"])}
	ev := a.auditEvent(audit.Event{
		EventType:  audit.EventOutboxDLQDeleted,
		TenantID:   tenantID,
		ActorType:  audit.ActorOperator,
		ActorID:    actorUserID,
		Entity:     audit.EntityRef{Type: "DLQ_MESSAGE", ID: eventID},
		Decision:   audit.DecisionDeny,
		ReasonCode: reason,
		CreatedAt:  a.Now(),
		Summary: map[string]any{
			"stream": stream,
		},
	})
	return a.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                auditapp.BuildEventRow(ev),
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Delete: &kv.DeleteOp{
			Key:                 key,
			ConditionExpression: "attribute_exists(PK) AND attribute_exists(SK)",
		}},
	})
}

func (a *OutboxDLQAdmin) Purge(ctx context.Context, stream, reason, tenantID, actorUserID string) (int32, error) {
	rows, err := a.List(ctx, stream, 100)
	if err != nil {
		return 0, err
	}
	var deleted int32
	for _, row := range rows {
		if err := a.Delete(ctx, stream, row.EventID, reason, tenantID, actorUserID); err != nil {
			return deleted, err
		}
		deleted++
	}
	_ = a.Recorder.Record(ctx, audit.Event{
		EventType:  audit.EventOutboxDLQPurged,
		TenantID:   tenantID,
		ActorType:  audit.ActorOperator,
		ActorID:    actorUserID,
		Entity:     audit.EntityRef{Type: "DLQ_STREAM", ID: stream},
		Decision:   audit.DecisionDeny,
		ReasonCode: reason,
		CreatedAt:  a.Now(),
		Summary: map[string]any{
			"deleted_count": deleted,
		},
	})
	return deleted, nil
}

func (a *OutboxDLQAdmin) findRaw(ctx context.Context, stream, eventID string) (map[string]any, error) {
	now := a.Now()
	remaining := int32(outboxDLQLookupScanCap)
	hitScanCap := false
	for i := 0; i < outboxDLQLookbackDays && remaining > 0; i++ {
		pk := outbox.DLQPK(stream, now.AddDate(0, 0, -i))
		var startKey *kv.Key
		for remaining > 0 {
			limit := minInt32(remaining, outboxDLQLookupPageLimit)
			page, err := a.KV.Query(ctx, kv.QueryRequest{
				KeyConditionExpression: "PK = :pk",
				ExpressionAttributeValues: kv.Values{
					":pk": pk,
				},
				Limit:             limit,
				ExclusiveStartKey: startKey,
			})
			if err != nil {
				return nil, err
			}
			remaining -= int32(len(page.Items))
			for _, item := range page.Items {
				var raw map[string]any
				if err := item.Unmarshal(&raw); err != nil {
					return nil, err
				}
				if fmt.Sprint(raw["event_id"]) == eventID {
					raw["_dlq_pk"] = pk
					raw["_dlq_sk"] = fmt.Sprint(item.Get("SK"))
					return raw, nil
				}
			}
			if remaining <= 0 && (page.LastEvaluatedKey != nil || i+1 < outboxDLQLookbackDays) {
				hitScanCap = true
				break
			}
			if page.LastEvaluatedKey == nil {
				break
			}
			startKey = page.LastEvaluatedKey
		}
	}
	if hitScanCap {
		return nil, ErrOutboxDLQLookupScanLimit
	}
	return nil, kv.ErrNotFound
}

func (a *OutboxDLQAdmin) auditEvent(ev audit.Event) audit.Event {
	ev.ID = randid.New()
	ev.CreatedAt = a.Now().UTC()
	return ev
}

func decodeOutboxDLQRow(row kv.Row) (OutboxDLQRow, error) {
	var raw map[string]any
	if err := row.Unmarshal(&raw); err != nil {
		return OutboxDLQRow{}, err
	}
	out := OutboxDLQRow{
		Stream:    fmt.Sprint(raw["stream"]),
		EventID:   fmt.Sprint(raw["event_id"]),
		EventType: fmt.Sprint(raw["event_type"]),
		TenantID:  fmt.Sprint(raw["tenant_id"]),
		LastError: firstString(raw["failure_reason"], raw["last_error"], raw["error"]),
	}
	if body, ok := raw["body"].([]byte); ok {
		out.Body = bytes.Clone(body)
	}
	if n, err := strconv.Atoi(fmt.Sprint(raw["attempts"])); err == nil {
		out.Attempts = int32(n)
	}
	if sk := fmt.Sprint(row.Get("SK")); sk != "" {
		if ts, _, ok := strings.Cut(sk, "#"); ok {
			out.PartitionTS, _ = time.Parse(time.RFC3339Nano, ts)
		}
	}
	if moved := fmt.Sprint(raw["moved_at"]); moved != "" {
		out.LastFailedAt, _ = time.Parse(time.RFC3339Nano, moved)
		out.FirstFailedAt = out.LastFailedAt
	}
	if shard := fmt.Sprint(raw["shard"]); shard != "" && shard != "<nil>" {
		out.Shard = shard
	}
	return out, nil
}

func firstString(values ...any) string {
	for _, v := range values {
		s := fmt.Sprint(v)
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func bodySHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
