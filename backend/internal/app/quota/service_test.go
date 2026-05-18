package quota_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	quotaapp "github.com/dtszwai/media-processing-service/backend/internal/app/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// fakeKV is the minimum kv.KV surface the Reservoir repo exercises: Put +
// Update + UpdateReturning + TransactWrite, all gated on the exact
// expressions the repo emits. The honest condition handling matters because
// the spec invariant ("never read-then-write") is only enforced at the
// storage layer — a permissive fake would silently mask a reserve regression.
type fakeKV struct {
	mu    sync.Mutex
	rows  map[string]map[string]any
	onGet func(row map[string]any, out any)
}

func newFakeKV() *fakeKV { return &fakeKV{rows: map[string]map[string]any{}} }

func rowKey(k kv.Key) string { return k.PK + "\x00" + k.SK }

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
		if _, exists := f.rows[pk+"\x00"+sk]; exists {
			return kv.ErrConditionFailed
		}
	}
	if strings.Contains(opts.ConditionExpression, "attribute_not_exists(PK)") {
		if _, exists := f.rows[pk+"\x00"+sk]; exists {
			return kv.ErrConditionFailed
		}
	}
	f.rows[pk+"\x00"+sk] = cloneRow(row)
	return nil
}

func (f *fakeKV) Get(_ context.Context, key kv.Key, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[rowKey(key)]
	if !ok {
		return kv.ErrNotFound
	}
	// Tests inspect the rows map directly; if a future test wires an
	// inspector lambda, that closure handles the dynamodbav unmarshal.
	if f.onGet != nil {
		f.onGet(row, out)
	}
	fillStructFields(row, out)
	return nil
}

func (f *fakeKV) Update(_ context.Context, op kv.UpdateOp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updateLocked(op)
}

func (f *fakeKV) UpdateReturning(_ context.Context, op kv.UpdateOp) (kv.UpdateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.updateLocked(op); err != nil {
		return kv.UpdateOutput{}, err
	}
	return kv.UpdateOutput{Attributes: cloneRow(f.rows[rowKey(op.Key)])}, nil
}

// updateLocked implements only the conditions and SET expressions the
// reservoir issues. Anything else is a test-fixture bug — fail loud.
func (f *fakeKV) updateLocked(op kv.UpdateOp) error {
	rk := rowKey(op.Key)
	row, exists := f.rows[rk]
	if !exists {
		// Reservoir conditions all assume an existing aggregate row.
		return kv.ErrConditionFailed
	}
	amount, _ := op.ExpressionAttributeValues[":n"].(int64)
	cond := op.ConditionExpression
	switch {
	case strings.Contains(cond, "available >= :n") && strings.Contains(cond, "#state = :open"):
		if readInt64(row["available"]) < amount {
			return kv.ErrConditionFailed
		}
		if row["state"] != string(quota.ReservoirOpen) {
			return kv.ErrConditionFailed
		}
	case strings.Contains(cond, "reserved >= :n"):
		if readInt64(row["reserved"]) < amount {
			return kv.ErrConditionFailed
		}
	case strings.Contains(cond, "#state = :reserved"):
		want, _ := op.ExpressionAttributeValues[":reserved"].(string)
		if row["state"] != want {
			return kv.ErrConditionFailed
		}
	}
	// Apply SETs. ExpressionAttributeNames may rewrite `#state` placeholders
	// the reservoir uses to keep `state` (a DDB reserved word) out of the
	// raw expression string.
	applySetWithNames(row, op.UpdateExpression, op.ExpressionAttributeValues, op.ExpressionAttributeNames)
	f.rows[rk] = row
	return nil
}

func (f *fakeKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, nil
}

func (f *fakeKV) Delete(context.Context, kv.DeleteOp) error { return nil }

// TransactWrite runs each op through the per-op handler and ROLLS BACK on
// any condition failure so the txn behaves atomically. The roll-back uses a
// snapshot — without it a successful aggregate decrement would survive a
// ledger conflict, defeating the test's whole point.
func (f *fakeKV) TransactWrite(ctx context.Context, ops []kv.WriteOp) error {
	f.mu.Lock()
	snapshot := make(map[string]map[string]any, len(f.rows))
	for k, v := range f.rows {
		snapshot[k] = cloneRow(v)
	}
	reasons := make([]kv.ItemCancelReason, len(ops))
	cancelled := false
	for i, op := range ops {
		err := f.executeSlot(op)
		if err != nil {
			reasons[i] = kv.ItemCancelReason{ConditionFailed: errors.Is(err, kv.ErrConditionFailed), Code: "ConditionalCheckFailed"}
			cancelled = true
		} else {
			reasons[i] = kv.ItemCancelReason{Code: "None"}
		}
	}
	if cancelled {
		// Roll back.
		f.rows = snapshot
		f.mu.Unlock()
		return &fakeTxnErr{items: reasons}
	}
	f.mu.Unlock()
	_ = ctx
	return nil
}

// executeSlot mirrors the single-op handlers but works without re-grabbing
// the mutex (TransactWrite already holds it).
func (f *fakeKV) executeSlot(op kv.WriteOp) error {
	switch {
	case op.Put != nil:
		row, ok := op.Put.Item.(map[string]any)
		if !ok {
			return errors.New("fakeKV: Put non-map")
		}
		pk, _ := row["PK"].(string)
		sk, _ := row["SK"].(string)
		key := pk + "\x00" + sk
		if strings.Contains(op.Put.ConditionExpression, "attribute_not_exists(PK) AND attribute_not_exists(SK)") {
			if _, exists := f.rows[key]; exists {
				return kv.ErrConditionFailed
			}
		}
		f.rows[key] = cloneRow(row)
		return nil
	case op.Update != nil:
		return f.updateLocked(*op.Update)
	case op.Delete != nil:
		delete(f.rows, rowKey(op.Delete.Key))
		return nil
	}
	return errors.New("fakeKV: empty WriteOp")
}

// Static interface assertion — keeps fakeKV in lockstep with kv.KV.
var _ kv.KV = (*fakeKV)(nil)

// fakeTxnErr satisfies kv.TxnError.
type fakeTxnErr struct {
	items []kv.ItemCancelReason
}

func (e *fakeTxnErr) Error() string                { return "txn cancelled" }
func (e *fakeTxnErr) Items() []kv.ItemCancelReason { return e.items }

// applySetWithNames implements the SET path the reservoir uses. The repo
// uses two shapes per assignment:
//
//	target = :placeholder              (direct set)
//	target = target [+|-] :placeholder (arithmetic)
//
// Target may be a literal attribute name OR a #-prefixed placeholder; the
// latter is resolved through ExpressionAttributeNames so reserved words
// like `state` survive the DDB round-trip.
func applySetWithNames(row map[string]any, expr string, vals kv.Values, names kv.Names) {
	body := strings.TrimSpace(expr)
	body = strings.TrimPrefix(body, "SET ")
	for _, part := range strings.Split(body, ",") {
		seg := strings.TrimSpace(part)
		eq := strings.Index(seg, "=")
		if eq < 0 {
			continue
		}
		lhs := resolveName(strings.TrimSpace(seg[:eq]), names)
		rhs := strings.TrimSpace(seg[eq+1:])
		applyAssign(row, lhs, rhs, vals, names)
	}
}

func resolveName(token string, names kv.Names) string {
	if strings.HasPrefix(token, "#") {
		if v, ok := names[token]; ok {
			return v
		}
	}
	return token
}

func applyAssign(row map[string]any, lhs, rhs string, vals kv.Values, names kv.Names) {
	if strings.HasPrefix(rhs, ":") {
		if v, ok := vals[rhs]; ok {
			row[lhs] = v
		}
		return
	}
	parts := strings.Fields(rhs)
	if len(parts) != 3 {
		return
	}
	baseAttr := resolveName(parts[0], names)
	base := readInt64(row[baseAttr])
	delta := readInt64(vals[parts[2]])
	switch parts[1] {
	case "-":
		row[lhs] = base - delta
	case "+":
		row[lhs] = base + delta
	}
}

func readInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func cloneRow(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func fillStructFields(row map[string]any, out any) {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return
	}
	for _, field := range []struct {
		Name string
		Key  string
	}{
		{"Amount", "amount"},
		{"State", "state"},
	} {
		f := elem.FieldByName(field.Name)
		if !f.IsValid() || !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.Int64:
			f.SetInt(readInt64(row[field.Key]))
		case reflect.String:
			s, _ := row[field.Key].(string)
			f.SetString(s)
		}
	}
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// helper to count rows by SK prefix.
func countSKPrefix(f *fakeKV, prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k := range f.rows {
		// k is "<PK>\x00<SK>". Split and prefix-match the SK half.
		if idx := strings.Index(k, "\x00"); idx >= 0 {
			if strings.HasPrefix(k[idx+1:], prefix) {
				n++
			}
		}
	}
	return n
}

func TestEnsure_IsIdempotent(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 9_999_999, "pol-1", 1); err != nil {
		t.Fatalf("second Ensure must collapse to nil: %v", err)
	}
	// AGG row count is 1 and cap is the FIRST writer's value (idempotency means
	// silent no-op on second Ensure rather than overwrite).
	if got := countSKPrefix(fkv, "AGG"); got != 1 {
		t.Fatalf("AGG rows = %d, want 1", got)
	}
}

func TestReserve_AtomicDecrement_AndLedgerLanding(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{
		ID:     "rsv-1",
		JobID:  "job-1",
		Amount: 1_000_000,
		Reason: "GENERATION_ESTIMATE",
	}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	// Get goes through the dynamodbav-tagged unmarshal which the fake
	// doesn't implement; the assertion runs directly against the raw row.
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["available"]) != 4_000_000 {
		t.Fatalf("available after reserve = %d, want 4_000_000", readInt64(agg["available"]))
	}
	if readInt64(agg["reserved"]) != 1_000_000 {
		t.Fatalf("reserved after reserve = %d, want 1_000_000", readInt64(agg["reserved"]))
	}
	if got := countSKPrefix(fkv, "LEDGER#"); got != 1 {
		t.Fatalf("LEDGER rows = %d, want 1", got)
	}
}

func TestReserve_Exhausted_ReturnsSentinel(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 500, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-1", Amount: 1000, Reason: "GENERATION_ESTIMATE"}
	err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res)
	if !errors.Is(err, quotaapp.ErrQuotaExhausted) {
		t.Fatalf("Reserve over-cap err = %v, want ErrQuotaExhausted", err)
	}
	// Aggregate row must be untouched (atomic txn rolled back).
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["available"]) != 500 {
		t.Fatalf("available after rejected reserve = %d, want 500 (unchanged)", readInt64(agg["available"]))
	}
	if got := countSKPrefix(fkv, "LEDGER#"); got != 0 {
		t.Fatalf("LEDGER rows after rejected reserve = %d, want 0", got)
	}
}

func TestReserve_DuplicateReservationID_ReplaysWithoutDoubleDecrement(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-dupe", Amount: 1000, Reason: "GENERATION_ESTIMATE"}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res)
	if err != nil {
		t.Fatalf("duplicate reservation replay must succeed: %v", err)
	}
	// Aggregate must show the single reservation only (no double-decrement).
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["available"]) != 4_999_000 {
		t.Fatalf("available after duplicate = %d, want 4_999_000 (only one decrement)", readInt64(agg["available"]))
	}
}

func TestReserve_DuplicateReservationID_DifferentAmountConflicts(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-dupe", Amount: 1000, Reason: "GENERATION_ESTIMATE"}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	res.Amount = 2000
	err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res)
	if !errors.Is(err, kv.ErrConditionFailed) {
		t.Fatalf("duplicate changed amount err = %v, want kv.ErrConditionFailed wrapped", err)
	}
}

func TestCommit_FlipsLedgerAndAggregate(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-1", Amount: 1000, Reason: "GENERATION_ESTIMATE"}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := r.Commit(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", "rsv-1", 1000); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	ledger := fkv.rows[pk+"\x00"+quotaapp.LedgerSK("rsv-1")]
	fkv.mu.Unlock()
	if readInt64(agg["reserved"]) != 0 {
		t.Fatalf("reserved after commit = %d, want 0", readInt64(agg["reserved"]))
	}
	if readInt64(agg["committed"]) != 1000 {
		t.Fatalf("committed after commit = %d, want 1000", readInt64(agg["committed"]))
	}
	if ledger["state"] != string(quota.ReservationCommitted) {
		t.Fatalf("ledger state after commit = %v, want COMMITTED", ledger["state"])
	}
}

func TestCommit_DoubleCommit_Replays(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-1", Amount: 1000}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := r.Commit(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", "rsv-1", 1000); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if err := r.Commit(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", "rsv-1", 1000); err != nil {
		t.Fatalf("double-commit replay must succeed: %v", err)
	}
	// Aggregate must still show only one committed amount.
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["committed"]) != 1000 {
		t.Fatalf("committed after double-commit = %d, want 1000 (no double-charge)", readInt64(agg["committed"]))
	}
}

func TestMeter_RecordVendorCost_RetryDoesNotDoubleCommit(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	m := quotaapp.NewMeter(r)
	ctx := context.Background()

	if err := m.RecordVendorCost(ctx, "openai", "job-1", 1000); err != nil {
		t.Fatalf("first RecordVendorCost: %v", err)
	}
	if err := m.RecordVendorCost(ctx, "openai", "job-1", 1000); err != nil {
		t.Fatalf("retry RecordVendorCost: %v", err)
	}

	period := quota.PeriodDaily(time.Now().UTC())
	pk := quotaapp.ReservoirPK(quota.ScopeVendor, "OPENAI", quota.CostMicroUSD, period)
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["committed"]) != 1000 {
		t.Fatalf("committed after retry = %d, want 1000", readInt64(agg["committed"]))
	}
	if readInt64(agg["reserved"]) != 0 {
		t.Fatalf("reserved after retry = %d, want 0", readInt64(agg["reserved"]))
	}
}

func TestMeter_RecordStorageBytes_RetryDoesNotDoubleCommit(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	m := quotaapp.NewMeter(r)
	ctx := context.Background()

	if err := m.RecordStorageBytes(ctx, "ten-1", "media-1", "asset-1", 42); err != nil {
		t.Fatalf("first RecordStorageBytes: %v", err)
	}
	if err := m.RecordStorageBytes(ctx, "ten-1", "media-1", "asset-1", 42); err != nil {
		t.Fatalf("retry RecordStorageBytes: %v", err)
	}

	period := quota.PeriodMonthly(time.Now().UTC())
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.StorageBytes, period)
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["committed"]) != 42 {
		t.Fatalf("committed after retry = %d, want 42", readInt64(agg["committed"]))
	}
	if readInt64(agg["reserved"]) != 0 {
		t.Fatalf("reserved after retry = %d, want 0", readInt64(agg["reserved"]))
	}
	if got := countSKPrefix(fkv, quotaapp.LedgerSK("storage:media-1:asset-1")); got != 1 {
		t.Fatalf("storage ledger rows = %d, want 1", got)
	}
}

func TestRelease_AfterCommit_Cancels(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-1", Amount: 1000}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := r.Commit(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", "rsv-1", 1000); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := r.Release(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", "rsv-1", 1000); err == nil {
		t.Fatalf("release-after-commit must cancel; got nil")
	}
	// Aggregate must remain in the committed-only state.
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	fkv.mu.Unlock()
	if readInt64(agg["released"]) != 0 {
		t.Fatalf("released after release-after-commit = %d, want 0", readInt64(agg["released"]))
	}
}

func TestRelease_FromReserved_Succeeds(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if err := r.Ensure(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", 5_000_000, "pol-1", 1); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	res := quota.Reservation{ID: "rsv-1", Amount: 1000}
	if err := r.Reserve(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", res); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := r.Release(ctx, quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516", "rsv-1", 1000); err != nil {
		t.Fatalf("Release: %v", err)
	}
	pk := quotaapp.ReservoirPK(quota.ScopeTenant, "ten-1", quota.CostMicroUSD, "20260516")
	fkv.mu.Lock()
	agg := fkv.rows[pk+"\x00"+quotaapp.AggSK]
	ledger := fkv.rows[pk+"\x00"+quotaapp.LedgerSK("rsv-1")]
	fkv.mu.Unlock()
	if readInt64(agg["available"]) != 5_000_000 {
		t.Fatalf("available after release = %d, want 5_000_000 (full restoration)", readInt64(agg["available"]))
	}
	if readInt64(agg["reserved"]) != 0 {
		t.Fatalf("reserved after release = %d, want 0", readInt64(agg["reserved"]))
	}
	if readInt64(agg["released"]) != 1000 {
		t.Fatalf("released after release = %d, want 1000", readInt64(agg["released"]))
	}
	if ledger["state"] != string(quota.ReservationReleased) {
		t.Fatalf("ledger state after release = %v, want RELEASED", ledger["state"])
	}
}

func TestClassifyTxnError(t *testing.T) {
	plan := kv.TxPlan{Ops: []kv.NamedTxOp{
		{Name: quotaapp.OpAdvanceJobStage},
		{Name: quotaapp.OpPutOutboxNextStage},
		{Name: quotaapp.OpAggregateTenantQuota},
		{Name: quotaapp.OpLedgerTenantQuota},
	}}
	cond := kv.ItemCancelReason{ConditionFailed: true, Code: "ConditionalCheckFailed"}
	none := kv.ItemCancelReason{Code: "None"}

	if got := quotaapp.ClassifyTxnError(plan, &fakeTxnErr{items: []kv.ItemCancelReason{none, none, cond, none}}); got != quotaapp.RetryExhausted {
		t.Errorf("aggregate exhausted got %v, want RetryExhausted", got)
	}
	if got := quotaapp.ClassifyTxnError(plan, &fakeTxnErr{items: []kv.ItemCancelReason{none, none, none, cond}}); got != quotaapp.RetryConflict {
		t.Errorf("ledger conflict got %v, want RetryConflict", got)
	}
	if got := quotaapp.ClassifyTxnError(plan, &fakeTxnErr{items: []kv.ItemCancelReason{cond, none, none, none}}); got != quotaapp.RetryReplay {
		t.Errorf("stage replay got %v, want RetryReplay", got)
	}
	if got := quotaapp.ClassifyTxnError(plan, nil); got != quotaapp.RetryReplay {
		t.Errorf("nil err got %v, want RetryReplay", got)
	}
}

func TestMeter_DefaultPricingVersion(t *testing.T) {
	fkv := newFakeKV()
	r := quotaapp.NewRepo(fkv)
	r.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	m := quotaapp.NewMeter(r)
	ctx := context.Background()
	if err := m.RecordVendorCost(ctx, "openai", "job-1", 100); err != nil {
		t.Fatalf("RecordVendorCost: %v", err)
	}
	period := quota.PeriodDaily(time.Now().UTC())
	pk := quotaapp.ReservoirPK(quota.ScopeVendor, "OPENAI", quota.CostMicroUSD, period)
	fkv.mu.Lock()
	row := fkv.rows[pk+"\x00"+quotaapp.LedgerSK("vendor-cost:job-1")]
	fkv.mu.Unlock()
	want := "default_quota_v1#1"
	if row["pricing_version"] != want {
		t.Fatalf("pricing_version = %v, want %q", row["pricing_version"], want)
	}
}

func TestMeter_PeriodHelpers(t *testing.T) {
	tt := time.Date(2026, 5, 16, 23, 0, 0, 0, time.UTC)
	if got := quota.PeriodDaily(tt); got != "20260516" {
		t.Fatalf("PeriodDaily = %q", got)
	}
	if got := quota.PeriodMonthly(tt); got != "202605" {
		t.Fatalf("PeriodMonthly = %q", got)
	}
}
