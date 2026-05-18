package audit_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// fakeKV is the minimum KV surface the audit Recorder uses: Put with a
// conditional expression. It honours the exact condition the recorder emits
// so a duplicate Put returns ErrConditionFailed (which Record collapses to
// nil) — a permissive fake would hide the immutability invariant.
type fakeKV struct {
	mu   sync.Mutex
	rows map[string]map[string]any
}

func newFakeKV() *fakeKV { return &fakeKV{rows: map[string]map[string]any{}} }

func (f *fakeKV) key(pk, sk string) string { return pk + "\x00" + sk }

func (f *fakeKV) Put(_ context.Context, item kv.Item, opts kv.PutOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := item.(map[string]any)
	if !ok {
		return errors.New("fakeKV: only map items supported")
	}
	pk, _ := row["PK"].(string)
	sk, _ := row["SK"].(string)
	if strings.Contains(opts.ConditionExpression, "attribute_not_exists(PK) AND attribute_not_exists(SK)") {
		if _, exists := f.rows[f.key(pk, sk)]; exists {
			return kv.ErrConditionFailed
		}
	}
	f.rows[f.key(pk, sk)] = cloneRow(row)
	return nil
}

func (f *fakeKV) Get(context.Context, kv.Key, any) error { return kv.ErrNotFound }
func (f *fakeKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, nil
}
func (f *fakeKV) Update(context.Context, kv.UpdateOp) error { return nil }
func (f *fakeKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, nil
}
func (f *fakeKV) Delete(context.Context, kv.DeleteOp) error         { return nil }
func (f *fakeKV) TransactWrite(context.Context, []kv.WriteOp) error { return nil }

func cloneRow(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestRecord_WritesCanonicalRowWithGSIKeys asserts the row shape the
// downstream reader (and Terraform-managed GSIs) depend on. Drift here
// silently breaks per-entity / per-actor lookups even though writes keep
// succeeding.
func TestRecord_WritesCanonicalRowWithGSIKeys(t *testing.T) {
	k := newFakeKV()
	r := auditapp.NewDDB(k)
	r.Now = fixedClock(time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC))
	r.NewID = func() string { return "aud-1" }

	ev := auditapp.NewAPIKeyCreated("tenant-1", "user-7", "key-42", "ci", "req-99")
	if err := r.Record(context.Background(), ev); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(k.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(k.rows))
	}
	var row map[string]any
	for _, v := range k.rows {
		row = v
	}
	if got, want := row["PK"].(string), "AUDIT#TENANT#tenant-1#20260515"; got != want {
		t.Fatalf("PK = %q, want %q", got, want)
	}
	if got := row["SK"].(string); !strings.HasPrefix(got, "2026-05-15T10:30:00Z#identity.api_key.created#key-42#aud-1") {
		t.Fatalf("SK = %q, want canonical layout", got)
	}
	if got := row["gsi_audit_entity_pk"].(string); got != "ENTITY#API_KEY#key-42" {
		t.Fatalf("gsi_audit_entity_pk = %q", got)
	}
	if got := row["gsi_audit_actor_pk"].(string); got != "ACTOR#USER#user-7" {
		t.Fatalf("gsi_audit_actor_pk = %q", got)
	}
	if _, ok := row["ttl_epoch"].(int64); !ok {
		t.Fatalf("ttl_epoch missing or wrong type: %T", row["ttl_epoch"])
	}
	if got := row["event_type"].(string); got != audit.EventIdentityAPIKeyCreated {
		t.Fatalf("event_type = %q", got)
	}
	if got := row["request_id"].(string); got != "req-99" {
		t.Fatalf("request_id = %q", got)
	}
}

// TestRecord_DuplicateWriteCollapsesToNil guards the immutability
// invariant: re-recording the same Event (same id, same SK) must look like
// success so handler retries don't surface storage internals.
func TestRecord_DuplicateWriteCollapsesToNil(t *testing.T) {
	k := newFakeKV()
	r := auditapp.NewDDB(k)
	r.Now = fixedClock(time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC))
	r.NewID = func() string { return "aud-fixed" }

	ev := auditapp.NewIdentityLoginSucceeded("user-1", "tenant-1", "req-1")
	if err := r.Record(context.Background(), ev); err != nil {
		t.Fatalf("Record first: %v", err)
	}
	if err := r.Record(context.Background(), ev); err != nil {
		t.Fatalf("Record duplicate: %v (want nil — duplicates must collapse)", err)
	}
	if got := len(k.rows); got != 1 {
		t.Fatalf("rows after duplicate = %d, want 1", got)
	}
}

// TestNewIdentityLoginFailed_KeysActorByEmail: failed logins lack a
// resolved user id; recording must still produce a queryable actor GSI key
// so operators can correlate brute-force attempts by submitted email.
func TestNewIdentityLoginFailed_KeysActorByEmail(t *testing.T) {
	k := newFakeKV()
	r := auditapp.NewDDB(k)
	r.Now = fixedClock(time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC))
	r.NewID = func() string { return "aud-2" }

	ev := auditapp.NewIdentityLoginFailed("alice@example.com", "BAD_PASSWORD", "req-1")
	if err := r.Record(context.Background(), ev); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var row map[string]any
	for _, v := range k.rows {
		row = v
	}
	if got := row["gsi_audit_actor_pk"].(string); got != "ACTOR#USER#alice@example.com" {
		t.Fatalf("gsi_audit_actor_pk = %q", got)
	}
	if got := row["decision"].(string); got != string(audit.DecisionDeny) {
		t.Fatalf("decision = %q", got)
	}
	if got := row["reason_code"].(string); got != "BAD_PASSWORD" {
		t.Fatalf("reason_code = %q", got)
	}
}

// TestNoopRecorder_DiscardsEvents pins the contract that NoopRecorder satisfies
// the same Recorder port as the DDB-backed implementation.
func TestNoopRecorder_DiscardsEvents(t *testing.T) {
	var r auditapp.Recorder = auditapp.NoopRecorder{}
	if err := r.Record(context.Background(), auditapp.NewIdentityLoginSucceeded("u", "t", "r")); err != nil {
		t.Fatalf("Noop returned err: %v", err)
	}
}
