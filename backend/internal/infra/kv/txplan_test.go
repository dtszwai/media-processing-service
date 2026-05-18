package kv_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// recordingKV captures the ops a plan submits so the test can assert order.
type recordingKV struct {
	kv.KV
	got []kv.WriteOp
}

func (r *recordingKV) TransactWrite(_ context.Context, ops []kv.WriteOp) error {
	r.got = append([]kv.WriteOp(nil), ops...)
	return nil
}

// stubTxnErr lets ClassifyByName traverse a synthetic cancellation.
type stubTxnErr struct{ items []kv.ItemCancelReason }

func (e *stubTxnErr) Error() string                { return "txn cancelled" }
func (e *stubTxnErr) Items() []kv.ItemCancelReason { return e.items }
func cancelOK() kv.ItemCancelReason                { return kv.ItemCancelReason{Code: "None"} }
func cancelCond() kv.ItemCancelReason {
	return kv.ItemCancelReason{ConditionFailed: true, Code: "ConditionalCheckFailed"}
}

func TestTxPlan_Execute_PreservesOpOrder(t *testing.T) {
	plan := kv.TxPlan{
		Name: "test.plan",
		Ops: []kv.NamedTxOp{
			{Name: "first", Op: kv.WriteOp{Put: &kv.PutOp{Item: map[string]any{"PK": "1"}}}},
			{Name: "second", Op: kv.WriteOp{Update: &kv.UpdateOp{Key: kv.Key{PK: "2"}}}},
			{Name: "third", Op: kv.WriteOp{Delete: &kv.DeleteOp{Key: kv.Key{PK: "3"}}}},
		},
	}
	rec := &recordingKV{}
	if err := plan.Execute(context.Background(), rec); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rec.got) != 3 {
		t.Fatalf("got %d ops, want 3", len(rec.got))
	}
	if rec.got[0].Put == nil || rec.got[0].Put.Item.(map[string]any)["PK"] != "1" {
		t.Errorf("slot 0 mismatch: %+v", rec.got[0])
	}
	if rec.got[1].Update == nil || rec.got[1].Update.Key.PK != "2" {
		t.Errorf("slot 1 mismatch: %+v", rec.got[1])
	}
	if rec.got[2].Delete == nil || rec.got[2].Delete.Key.PK != "3" {
		t.Errorf("slot 2 mismatch: %+v", rec.got[2])
	}
}

func TestClassifyByName_MapsFailedSlotToName(t *testing.T) {
	plan := kv.TxPlan{
		Name: "test.plan",
		Ops: []kv.NamedTxOp{
			{Name: "advance_job_stage"},
			{Name: "put_outbox_next_stage"},
			{Name: "aggregate_tenant_quota"},
			{Name: "ledger_tenant_quota"},
		},
	}

	cases := []struct {
		label string
		items []kv.ItemCancelReason
		want  kv.TxOpName
		found bool
	}{
		{"ledger conflict (trailing)",
			[]kv.ItemCancelReason{cancelOK(), cancelOK(), cancelOK(), cancelCond()},
			"ledger_tenant_quota", true},
		{"aggregate exhausted",
			[]kv.ItemCancelReason{cancelOK(), cancelOK(), cancelCond(), cancelOK()},
			"aggregate_tenant_quota", true},
		{"job stage replay",
			[]kv.ItemCancelReason{cancelCond(), cancelOK(), cancelOK(), cancelOK()},
			"advance_job_stage", true},
		{"no condition failure",
			[]kv.ItemCancelReason{cancelOK(), cancelOK(), cancelOK(), cancelOK()},
			"", false},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			name, ok := kv.ClassifyByName(plan, &stubTxnErr{items: tc.items})
			if ok != tc.found {
				t.Fatalf("found=%v, want %v", ok, tc.found)
			}
			if name != tc.want {
				t.Errorf("name=%q, want %q", name, tc.want)
			}
		})
	}
}

func TestClassifyByName_NilErrorReturnsEmpty(t *testing.T) {
	plan := kv.TxPlan{Name: "p", Ops: []kv.NamedTxOp{{Name: "x"}}}
	if name, ok := kv.ClassifyByName(plan, nil); ok || name != "" {
		t.Errorf("nil err: got (%q,%v), want (\"\",false)", name, ok)
	}
}

func TestClassifyByName_IndexBeyondPlanReturnsEmptyButFound(t *testing.T) {
	plan := kv.TxPlan{Name: "p", Ops: []kv.NamedTxOp{{Name: "only"}}}
	// Cancellation reports two slots but the plan only declared one — the
	// classifier still flags the failure but can't name it.
	items := []kv.ItemCancelReason{cancelOK(), cancelCond()}
	name, ok := kv.ClassifyByName(plan, &stubTxnErr{items: items})
	if !ok {
		t.Errorf("expected found=true even when index >= len(plan.Ops)")
	}
	if name != "" {
		t.Errorf("name=%q, want empty when slot out of range", name)
	}
}

// Sanity: TxPlan.Execute surfaces the KV error verbatim so callers can
// errors.Is/As the cancellation through the plan boundary.
func TestTxPlan_Execute_SurfacesError(t *testing.T) {
	want := errors.New("boom")
	rec := &failingKV{err: want}
	plan := kv.TxPlan{Ops: []kv.NamedTxOp{{Name: "x", Op: kv.WriteOp{Put: &kv.PutOp{}}}}}
	if got := plan.Execute(context.Background(), rec); !errors.Is(got, want) {
		t.Errorf("Execute err = %v, want %v", got, want)
	}
}

type failingKV struct {
	kv.KV
	err error
}

func (f *failingKV) TransactWrite(context.Context, []kv.WriteOp) error { return f.err }
