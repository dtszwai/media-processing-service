package quota_test

import (
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
)

func TestMetricConstants(t *testing.T) {
	cases := map[quota.Metric]string{
		quota.CostMicroUSD:     "COST_MICRO_USD",
		quota.Requests:         "REQUESTS",
		quota.GeneratedOutputs: "GENERATED_OUTPUTS",
		quota.StorageBytes:     "STORAGE_BYTES",
		quota.ProviderCalls:    "PROVIDER_CALLS",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Fatalf("Metric drift: got %q want %q", got, want)
		}
	}
}

func TestScopeAndStateEnums(t *testing.T) {
	if quota.ScopeTenant != "TENANT" || quota.ScopeAPIKey != "APIKEY" {
		t.Fatalf("ScopeType drift")
	}
	if quota.ReservoirOpen != "OPEN" || quota.ReservoirExhausted != "EXHAUSTED" || quota.ReservoirClosed != "CLOSED" {
		t.Fatalf("ReservoirState drift")
	}
	if quota.ReservationReserved != "RESERVED" || quota.ReservationCommitted != "COMMITTED" ||
		quota.ReservationReleased != "RELEASED" || quota.ReservationReconciled != "RECONCILED" {
		t.Fatalf("ReservationState drift")
	}
}

func TestReservoirInvariantArithmetic(t *testing.T) {
	// Document the invariant: Available = Cap - Reserved - Committed + Released.
	// The domain doesn't enforce it (the storage layer does, conditionally),
	// but a downstream consumer reading these fields should be able to rely on
	// it. This test pins the contract so a field rename can't silently break it.
	r := quota.Reservoir{Cap: 100, Reserved: 30, Committed: 10, Released: 5}
	want := r.Cap - r.Reserved - r.Committed + r.Released
	r.Available = want
	if r.Available != 65 {
		t.Fatalf("reservoir arithmetic invariant: got %d want 65", r.Available)
	}
}
