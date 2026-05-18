package ddb_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/app/generation/ddb"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// fakeKV is the minimum KV subset ddb.ResourceLessor uses: Update with
// conditional expression, Delete with conditional expression, and Query on
// the gsi_lease_expiry GSI. It honours the exact conditions ddb.ResourceLessor
// emits — the tests would silently pass against a permissive fake, hiding
// real bugs in the prod path.
type fakeKV struct {
	mu   sync.Mutex
	rows map[string]map[string]any
}

func newFakeKV() *fakeKV { return &fakeKV{rows: map[string]map[string]any{}} }

func rowKey(k kv.Key) string { return k.PK + "\x00" + k.SK }

func (f *fakeKV) Put(_ context.Context, item kv.Item, _ kv.PutOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := item.(map[string]any)
	if !ok {
		return errors.New("fakeKV: only map items supported")
	}
	pk, _ := row["PK"].(string)
	sk, _ := row["SK"].(string)
	f.rows[pk+"\x00"+sk] = copyRow(row)
	return nil
}

func (f *fakeKV) Get(_ context.Context, key kv.Key, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[rowKey(key)]; !ok {
		return kv.ErrNotFound
	}
	return nil
}

func (f *fakeKV) Update(_ context.Context, op kv.UpdateOp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rk := rowKey(op.Key)
	row, exists := f.rows[rk]
	if !checkCondition(op.ConditionExpression, op.ExpressionAttributeValues, row, exists) {
		return kv.ErrConditionFailed
	}
	if row == nil {
		row = map[string]any{"PK": op.Key.PK, "SK": op.Key.SK}
	}
	applySet(row, op.UpdateExpression, op.ExpressionAttributeValues)
	f.rows[rk] = row
	return nil
}

func (f *fakeKV) UpdateReturning(ctx context.Context, op kv.UpdateOp) (kv.UpdateOutput, error) {
	if err := f.Update(ctx, op); err != nil {
		return kv.UpdateOutput{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return kv.UpdateOutput{Attributes: copyRow(f.rows[rowKey(op.Key)])}, nil
}

func (f *fakeKV) Delete(_ context.Context, op kv.DeleteOp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rk := rowKey(op.Key)
	row, exists := f.rows[rk]
	if !checkCondition(op.ConditionExpression, op.ExpressionAttributeValues, row, exists) {
		return kv.ErrConditionFailed
	}
	delete(f.rows, rk)
	return nil
}

func (f *fakeKV) Query(_ context.Context, req kv.QueryRequest) (kv.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Index != "gsi_lease_expiry" {
		return kv.QueryResult{}, errors.New("fakeKV: only gsi_lease_expiry supported")
	}
	wantPK, _ := req.ExpressionAttributeValues[":pk"].(string)
	wantSKBelow, _ := req.ExpressionAttributeValues[":now"].(string)
	var items []kv.Row
	for _, row := range f.rows {
		if pk, _ := row["gsi_lease_pk"].(string); pk != wantPK {
			continue
		}
		if sk, _ := row["gsi_lease_sk"].(string); sk >= wantSKBelow {
			continue
		}
		items = append(items, fakeRow(copyRow(row)))
	}
	return kv.QueryResult{Items: items}, nil
}

func (f *fakeKV) TransactWrite(_ context.Context, _ []kv.WriteOp) error {
	return errors.New("fakeKV: TransactWrite not supported")
}

type fakeRow map[string]any

func (r fakeRow) Unmarshal(out any) error {
	// Only support the shape ddb.ResourceLessor.SweepExpired reads.
	t, ok := out.(*struct {
		PK       string `dynamodbav:"PK"`
		SK       string `dynamodbav:"SK"`
		TTLEpoch int64  `dynamodbav:"ttl_epoch"`
	})
	if !ok {
		return errors.New("fakeRow: unsupported unmarshal target")
	}
	t.PK, _ = r["PK"].(string)
	t.SK, _ = r["SK"].(string)
	switch v := r["ttl_epoch"].(type) {
	case int64:
		t.TTLEpoch = v
	case int:
		t.TTLEpoch = int64(v)
	}
	return nil
}

func (r fakeRow) Get(name string) any { return r[name] }

// checkCondition supports only the expressions ddb.ResourceLessor emits.
// Each branch maps to one Acquire/Renew/Release shape.
func checkCondition(expr string, vals kv.Values, row map[string]any, exists bool) bool {
	if expr == "" {
		return true
	}
	switch {
	case strings.Contains(expr, "attribute_not_exists(PK)") && strings.Contains(expr, "ttl_epoch < :now_unix"):
		// Acquire condition.
		if !exists {
			return true
		}
		nowUnix, _ := vals[":now_unix"].(int64)
		expUnix, _ := row["ttl_epoch"].(int64)
		return expUnix < nowUnix
	case strings.Contains(expr, "held_by = :me"):
		// Renew/Release condition.
		if !exists {
			return false
		}
		me, _ := vals[":me"].(string)
		held, _ := row["held_by"].(string)
		return me == held
	case strings.Contains(expr, "ttl_epoch = :seen"):
		// Sweep delete condition: row still has the expiry the sweep saw.
		if !exists {
			return false
		}
		seen, _ := vals[":seen"].(int64)
		cur, _ := row["ttl_epoch"].(int64)
		return seen == cur
	}
	return false
}

// applySet is the minimal UpdateExpression parser. It only honors comma-
// separated `attr = :val` assignments after the leading `SET `.
func applySet(row map[string]any, expr string, vals kv.Values) {
	body := strings.TrimSpace(expr)
	body = strings.TrimPrefix(body, "SET ")
	for _, part := range strings.Split(body, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		attr := strings.TrimSpace(kv[0])
		placeholder := strings.TrimSpace(kv[1])
		if v, ok := vals[placeholder]; ok {
			row[attr] = v
		}
	}
}

func copyRow(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// fixedClock returns the same time every call. Tests advance the clock by
// reassigning lessor.Now.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestResourceLessor_RaceAcquire_OnlyOneWins: two workers call Acquire
// concurrently for the same key. Exactly one succeeds; the loser gets a
// transient "RESOURCE_CAPACITY_UNAVAILABLE" so SQS retries.
func TestResourceLessor_RaceAcquire_OnlyOneWins(t *testing.T) {
	kv := newFakeKV()
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

	a := ddb.NewResourceLessor(kv)
	a.Now = fixedClock(now)
	a.InstanceID = "worker-A"

	b := ddb.NewResourceLessor(kv)
	b.Now = fixedClock(now)
	b.InstanceID = "worker-B"

	ctx := context.Background()
	req := genapp.LeaseRequest{ResourceClass: generation.ResourceProvider, TenantID: "t1", JobID: "j1", TTL: time.Minute}

	results := make(chan error, 2)
	var wins atomic.Int32
	for _, l := range []*ddb.ResourceLessor{a, b} {
		l := l
		go func() {
			_, err := l.AcquireResource(ctx, req)
			if err == nil {
				wins.Add(1)
			}
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		<-results
	}
	if got := wins.Load(); got != 1 {
		t.Fatalf("wins = %d, want exactly 1", got)
	}
}

// TestResourceLessor_HolderCrash_ReaperSweepUnblocks: worker A acquires
// and crashes (never releases). Reaper sweep after the TTL expires drops the
// row. Worker B can then acquire.
func TestResourceLessor_HolderCrash_ReaperSweepUnblocks(t *testing.T) {
	kv := newFakeKV()
	t0 := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

	a := ddb.NewResourceLessor(kv)
	a.Now = fixedClock(t0)
	a.InstanceID = "worker-A"

	ctx := context.Background()
	req := genapp.LeaseRequest{ResourceClass: generation.ResourceProvider, TenantID: "t1", JobID: "j1", TTL: time.Minute}
	if _, err := a.AcquireResource(ctx, req); err != nil {
		t.Fatalf("A acquire: %v", err)
	}
	// Worker A "crashes" — no Release.

	// B tries before TTL elapses → conflict.
	b := ddb.NewResourceLessor(kv)
	b.Now = fixedClock(t0.Add(30 * time.Second))
	b.InstanceID = "worker-B"
	if _, err := b.AcquireResource(ctx, req); err == nil {
		t.Fatalf("B acquired before TTL — capacity should still be held by A")
	}

	// Time moves past A's expiry. The sweep finds and deletes the row.
	sweeper := ddb.NewResourceLessor(kv)
	sweeper.Now = fixedClock(t0.Add(2 * time.Minute))
	res, err := sweeper.SweepExpired(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Scanned != 1 || res.Deleted != 1 {
		t.Fatalf("sweep result = %+v, want scanned=1 deleted=1", res)
	}

	// B retries after the sweep → succeeds.
	b.Now = fixedClock(t0.Add(2*time.Minute + time.Second))
	if _, err := b.AcquireResource(ctx, req); err != nil {
		t.Fatalf("B acquire after sweep: %v", err)
	}
}

// TestResourceLessor_RenewBeatsSweep_RaceCondition: holder renews just
// before the sweep walks past their row. The sweep's `ttl_epoch = :seen`
// guard must reject the delete so the renewed holder keeps their lease.
func TestResourceLessor_RenewBeatsSweep_RaceCondition(t *testing.T) {
	kv := newFakeKV()
	t0 := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

	holder := ddb.NewResourceLessor(kv)
	holder.Now = fixedClock(t0)
	holder.InstanceID = "worker-A"

	ctx := context.Background()
	req := genapp.LeaseRequest{ResourceClass: generation.ResourceProvider, TenantID: "t1", JobID: "j1", TTL: time.Minute}
	lease, err := holder.AcquireResource(ctx, req)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Sweeper runs at t0+90s — A's original expiry (t0+60s) is past.
	sweeper := ddb.NewResourceLessor(kv)
	sweeper.Now = fixedClock(t0.Add(90 * time.Second))

	// Before sweep deletes, the holder renews. Renew updates ttl_epoch
	// to a new value so the sweep's Delete-with-`ttl_epoch=:seen` guard
	// will refuse the row.
	holder.Now = fixedClock(t0.Add(70 * time.Second))
	if err := holder.RenewResource(ctx, lease.ID, lease.ResourceClass, lease.TenantID, time.Minute); err != nil {
		t.Fatalf("renew: %v", err)
	}

	res, err := sweeper.SweepExpired(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// The sweep's Query saw an expired snapshot, but the conditional delete
	// found ttl_epoch had moved — so deleted=0.
	if res.Deleted != 0 {
		t.Fatalf("sweep deleted=%d, want 0 (renew won the race)", res.Deleted)
	}
}

// TestResourceLessor_ReleaseClearsRow: a normal Release lets the next
// acquire succeed immediately, even before TTL would have elapsed.
func TestResourceLessor_ReleaseClearsRow(t *testing.T) {
	kv := newFakeKV()
	t0 := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

	l := ddb.NewResourceLessor(kv)
	l.Now = fixedClock(t0)
	l.InstanceID = "worker-A"
	ctx := context.Background()
	req := genapp.LeaseRequest{ResourceClass: generation.ResourceProvider, TenantID: "t1", JobID: "j1", TTL: time.Minute}
	lease, err := l.AcquireResource(ctx, req)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := l.ReleaseResource(ctx, lease); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Same instance can now re-acquire.
	if _, err := l.AcquireResource(ctx, req); err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
}
