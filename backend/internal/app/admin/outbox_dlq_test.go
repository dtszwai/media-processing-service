package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

type outboxDLQMapRow map[string]any

func (r outboxDLQMapRow) Get(name string) any { return r[name] }

func (r outboxDLQMapRow) Unmarshal(out any) error {
	dst, ok := out.(*map[string]any)
	if !ok {
		return errors.New("outboxDLQMapRow: unsupported unmarshal target")
	}
	cp := make(map[string]any, len(r))
	for k, v := range r {
		cp[k] = v
	}
	*dst = cp
	return nil
}

type outboxDLQFakeKV struct {
	rowsByPK map[string][]kv.Row
}

func (f *outboxDLQFakeKV) Put(context.Context, kv.Item, kv.PutOptions) error { return nil }
func (f *outboxDLQFakeKV) Get(context.Context, kv.Key, any) error            { return kv.ErrNotFound }
func (f *outboxDLQFakeKV) Update(context.Context, kv.UpdateOp) error         { return nil }
func (f *outboxDLQFakeKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, nil
}
func (f *outboxDLQFakeKV) Delete(context.Context, kv.DeleteOp) error         { return nil }
func (f *outboxDLQFakeKV) TransactWrite(context.Context, []kv.WriteOp) error { return nil }

func (f *outboxDLQFakeKV) Query(_ context.Context, req kv.QueryRequest) (kv.QueryResult, error) {
	pk, _ := req.ExpressionAttributeValues[":pk"].(string)
	rows := f.rowsByPK[pk]
	offset := 0
	if req.ExclusiveStartKey != nil && req.ExclusiveStartKey.ExtraAttrs != nil {
		offset, _ = strconv.Atoi(req.ExclusiveStartKey.ExtraAttrs["offset"])
	}
	if offset > len(rows) {
		offset = len(rows)
	}
	limit := len(rows) - offset
	if req.Limit > 0 && int(req.Limit) < limit {
		limit = int(req.Limit)
	}
	end := offset + limit
	out := kv.QueryResult{Items: rows[offset:end]}
	if end < len(rows) {
		out.LastEvaluatedKey = &kv.Key{PK: pk, ExtraAttrs: map[string]string{"offset": strconv.Itoa(end)}}
	}
	return out, nil
}

func TestOutboxDLQFindRawScansPastFirstPageWithinCap(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	pk := outbox.DLQPK(outbox.StreamMedia, now)
	rows := make([]kv.Row, outboxDLQLookupPageLimit+1)
	body := []byte(`{"ok":true}`)
	for i := range rows {
		eventID := fmt.Sprintf("event-%03d", i)
		if i == len(rows)-1 {
			eventID = "target-event"
		}
		rows[i] = outboxDLQMapRow{
			"PK":          pk,
			"SK":          fmt.Sprintf("2026-05-17T12:00:00.%09dZ#%s", i, eventID),
			"stream":      outbox.StreamMedia,
			"event_id":    eventID,
			"event_type":  "media.v1.process",
			"tenant_id":   "tenant-1",
			"body":        body,
			"body_sha256": bodySHA256(body),
		}
	}
	admin := NewOutboxDLQAdmin(&outboxDLQFakeKV{rowsByPK: map[string][]kv.Row{pk: rows}}, nil)
	admin.Now = func() time.Time { return now }

	raw, err := admin.findRaw(context.Background(), outbox.StreamMedia, "target-event")
	if err != nil {
		t.Fatalf("findRaw: %v", err)
	}
	if got := fmt.Sprint(raw["event_id"]); got != "target-event" {
		t.Fatalf("event_id = %q, want target-event", got)
	}
	if got := fmt.Sprint(raw["_dlq_pk"]); got != pk {
		t.Fatalf("_dlq_pk = %q, want %q", got, pk)
	}
}
