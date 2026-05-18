package main

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
)

// fakeRunner records the tenant IDs it was called with. runOnce calls Run
// concurrently per tenant, so the recorder is mutex-guarded; tests assert
// against the *set* of calls, not the order.
type fakeRunner struct {
	mu          sync.Mutex
	called      []string
	failTenants map[string]error
}

func (f *fakeRunner) Run(_ context.Context, tenantID string) (int, error) {
	f.mu.Lock()
	f.called = append(f.called, tenantID)
	f.mu.Unlock()
	if err, ok := f.failTenants[tenantID]; ok {
		return 0, err
	}
	return 1, nil
}

func (f *fakeRunner) calledSorted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.called...)
	sort.Strings(out)
	return out
}

// TestRunOnce_EmptyTenants verifies that an empty tenant list produces nil
// and never calls Run — no tenants configured means nothing to reap.
func TestRunOnce_EmptyTenants(t *testing.T) {
	r := &fakeRunner{}
	if err := runOnce(context.Background(), r, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if len(r.called) != 0 {
		t.Fatalf("expected 0 Run calls, got %d: %v", len(r.called), r.called)
	}
}

// TestRunOnce_MultiTenant verifies that each tenant in the list receives
// exactly one Run call. runOnce dispatches tenants concurrently, so order
// is non-deterministic; we compare sorted sets.
func TestRunOnce_MultiTenant(t *testing.T) {
	tenants := []string{"ten_a", "ten_b", "ten_c"}
	r := &fakeRunner{}
	if err := runOnce(context.Background(), r, tenants); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	got := r.calledSorted()
	if len(got) != len(tenants) {
		t.Fatalf("expected %d Run calls, got %d: %v", len(tenants), len(got), got)
	}
	want := append([]string(nil), tenants...)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRunOnce_OneFailDoesNotBlockOthers verifies that one tenant's failure
// does not short-circuit the remaining tenants. EventBridge should receive a
// failed invocation only after all tenants have been attempted.
func TestRunOnce_OneFailDoesNotBlockOthers(t *testing.T) {
	boom := errors.New("ddb timeout")
	tenants := []string{"ten_a", "ten_b", "ten_c"}
	r := &fakeRunner{failTenants: map[string]error{"ten_b": boom}}

	err := runOnce(context.Background(), r, tenants)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	// All three tenants must have been attempted despite ten_b failing.
	if got := r.calledSorted(); len(got) != 3 {
		t.Fatalf("expected 3 Run calls, got %d: %v", len(got), got)
	}
}

// TestRunOnce_AggregateErrorContainsFailingMessage verifies that the returned
// error wraps the original failure message so CloudWatch log insights can
// surface it.
func TestRunOnce_AggregateErrorContainsFailingMessage(t *testing.T) {
	boom := errors.New("ddb timeout")
	r := &fakeRunner{failTenants: map[string]error{"ten_x": boom}}

	err := runOnce(context.Background(), r, []string{"ten_x"})
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected errors.Is(err, boom) to be true; got %v", err)
	}
}
