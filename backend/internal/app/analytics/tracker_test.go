package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// stubKV implements kv.KV for Sink unit tests.
type stubKV struct {
	txnErr      error
	updateCalls int
}

func (k *stubKV) Put(context.Context, kv.Item, kv.PutOptions) error { return nil }
func (k *stubKV) Get(context.Context, kv.Key, any) error            { return kv.ErrNotFound }
func (k *stubKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, nil
}
func (k *stubKV) Update(_ context.Context, _ kv.UpdateOp) error {
	k.updateCalls++
	return nil
}
func (k *stubKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, nil
}
func (k *stubKV) Delete(context.Context, kv.DeleteOp) error         { return nil }
func (k *stubKV) TransactWrite(context.Context, []kv.WriteOp) error { return k.txnErr }

type fakeTxnErr struct {
	items []kv.ItemCancelReason
}

func (e *fakeTxnErr) Error() string                { return "txn cancelled" }
func (e *fakeTxnErr) Items() []kv.ItemCancelReason { return e.items }

func TestSinkApplyDuplicateLedgerStillUpserts(t *testing.T) {
	k := &stubKV{
		txnErr: &fakeTxnErr{items: []kv.ItemCancelReason{
			{ConditionFailed: true, Code: "ConditionalCheckFailed"},
			{Code: "None"},
		}},
	}
	sink := &Sink{KV: k}
	err := sink.Apply(context.Background(), Event{
		AnalyticsEventID: "ae_1",
		EventType:        EventTypeMediaView,
		DedupeKey:        "dk_1",
		TenantID:         "tenant-1",
		MediaID:          "media-1",
		OccurredAt:       time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Apply duplicate: %v", err)
	}
	if k.updateCalls != 2 {
		t.Fatalf("duplicate path update calls = %d, want 2", k.updateCalls)
	}
}

func TestSinkApplyDoesNotSwallowUnrelatedTransactionCancel(t *testing.T) {
	k := &stubKV{
		txnErr: errors.New("transaction conflict"),
	}
	sink := &Sink{KV: k}
	err := sink.Apply(context.Background(), Event{
		AnalyticsEventID: "ae_1",
		EventType:        EventTypeMediaView,
		DedupeKey:        "dk_1",
		TenantID:         "tenant-1",
		MediaID:          "media-1",
		OccurredAt:       time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected unrelated transaction cancel to be returned")
	}
	if k.updateCalls != 0 {
		t.Fatalf("unrelated cancel update calls = %d, want 0", k.updateCalls)
	}
}

// TestSinkApplyRequiresDedupeKey locks the contract: the ledger uniqueness
// primitive is DedupeKey, not AnalyticsEventID. Apply must reject envelopes
// missing the dedupe key so a malformed publisher can't silently bypass the
// consumer-side dedupe and double-count.
func TestSinkApplyRequiresDedupeKey(t *testing.T) {
	sink := &Sink{KV: &stubKV{}}
	err := sink.Apply(context.Background(), Event{
		EventType:  EventTypeMediaView,
		TenantID:   "tenant-1",
		MediaID:    "media-1",
		OccurredAt: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected error for missing dedupe_key")
	}
}

// TestComputeDedupeKeyStableWithinHour: a fast user retry inside the same
// hour produces the same DedupeKey so the consumer collapses them.
func TestComputeDedupeKeyStableWithinHour(t *testing.T) {
	a := ComputeDedupeKey(EventTypeMediaDownload, "t1", "m1", "ast1", "u1",
		time.Date(2026, 5, 12, 10, 3, 0, 0, time.UTC))
	b := ComputeDedupeKey(EventTypeMediaDownload, "t1", "m1", "ast1", "u1",
		time.Date(2026, 5, 12, 10, 57, 30, 0, time.UTC))
	if a != b {
		t.Fatalf("dedupe key drifted within the same hour: %q vs %q", a, b)
	}
}

// TestComputeDedupeKeyDiffersAcrossHours: separate hours legitimately count
// as separate events, so the dedupe key must change at the hour boundary.
func TestComputeDedupeKeyDiffersAcrossHours(t *testing.T) {
	a := ComputeDedupeKey(EventTypeMediaDownload, "t1", "m1", "ast1", "u1",
		time.Date(2026, 5, 12, 10, 59, 59, 0, time.UTC))
	b := ComputeDedupeKey(EventTypeMediaDownload, "t1", "m1", "ast1", "u1",
		time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC))
	if a == b {
		t.Fatalf("dedupe key did not change at hour boundary: %q", a)
	}
}

// TestComputeDedupeKeyDiffersByEventType: a MEDIA_VIEW and a MEDIA_DOWNLOAD
// on the same media in the same hour must dedupe to distinct keys so they
// land in different counter families.
func TestComputeDedupeKeyDiffersByEventType(t *testing.T) {
	ts := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	if ComputeDedupeKey(EventTypeMediaView, "t1", "m1", "", "u1", ts) ==
		ComputeDedupeKey(EventTypeMediaDownload, "t1", "m1", "", "u1", ts) {
		t.Fatal("VIEW and DOWNLOAD dedupe keys must differ")
	}
}

// fakePub captures the last envelope a tracker forwarded so the Track
// surface can be exercised without standing up SNS.
type fakePub struct {
	last Event
	err  error
}

func (p *fakePub) PublishAnalyticsEvent(_ context.Context, evt Event) error {
	if p.err != nil {
		return p.err
	}
	p.last = evt
	return nil
}

func TestSNSTrackerFillsMissingFields(t *testing.T) {
	pub := &fakePub{}
	tr := NewTracker(pub)
	tr.Now = func() time.Time { return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC) }
	err := tr.Track(context.Background(), Event{
		EventType:   EventTypeMediaView,
		TenantID:    "t1",
		MediaID:     "m1",
		AssetID:     "ast1",
		PrincipalID: "u1",
	})
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if pub.last.AnalyticsEventID == "" {
		t.Fatal("AnalyticsEventID was not auto-filled")
	}
	if pub.last.DedupeKey == "" {
		t.Fatal("DedupeKey was not auto-filled")
	}
	if pub.last.OccurredAt.IsZero() {
		t.Fatal("OccurredAt was not auto-filled")
	}
}
