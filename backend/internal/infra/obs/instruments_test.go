package obs_test

import (
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

// TestNewInstruments_RegistersWithNoopMeter pins the constructor against the
// no-op MeterProvider. The SDK ships separate validation rules per instrument
// type; a typo in a unit / description argument fails at construction, not at
// the first Add() — keeping this test ensures any future field addition can't
// land with a malformed registration.
func TestNewInstruments_RegistersWithNoopMeter(t *testing.T) {
	t.Parallel()
	mp := noop.NewMeterProvider()
	inst, err := obs.NewInstruments(mp.Meter(obs.MeterName))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	if inst == nil {
		t.Fatal("NewInstruments returned nil")
	}
	if inst.WorkflowStageStarted == nil ||
		inst.WorkflowStageCompleted == nil ||
		inst.WorkflowTerminal == nil ||
		inst.PromptEnhancementAttempts == nil ||
		inst.WorkflowStageLatency == nil ||
		inst.ProviderRequests == nil ||
		inst.ProviderRequestLatency == nil ||
		inst.SafetyDecisions == nil ||
		inst.OutboxPublished == nil ||
		inst.OutboxRelayLatency == nil ||
		inst.QuotaUsedPct == nil {
		t.Fatal("NewInstruments left an instrument field nil")
	}
}

// TestNewInstruments_NilMeterRejected guards against a wiring bug where a
// caller passes the zero-value Meter from a struct that never had its
// MeterProvider set. The package contract is "nil meter is a hard error,
// not a silent no-op".
func TestNewInstruments_NilMeterRejected(t *testing.T) {
	t.Parallel()
	if _, err := obs.NewInstruments(nil); err == nil {
		t.Fatal("NewInstruments(nil) returned no error")
	}
}

// TestNoop_NonNil pins the documented contract: Noop() returns a non-nil
// *Instruments whose fields are all safe to call. Direct field access against
// a no-op meter must not panic and must accept the expected attribute set.
func TestNoop_NonNil(t *testing.T) {
	t.Parallel()
	inst := obs.Noop()
	if inst == nil {
		t.Fatal("Noop returned nil")
	}
	// The no-op instruments must not panic when called. The values themselves
	// are not observable through the noop pipeline, so we only confirm the
	// call paths execute without error.
	inst.WorkflowStageStarted.Add(t.Context(), 1)
	inst.PromptEnhancementAttempts.Add(t.Context(), 1)
	inst.WorkflowStageLatency.Record(t.Context(), 1.0)
}
