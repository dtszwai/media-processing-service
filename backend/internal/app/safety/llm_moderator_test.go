package safety

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

func TestLLMModeratorPropagatesModelAndUsesPrefilter(t *testing.T) {
	model := &recordingModerationModel{out: ModerationModelResult{
		Decision:            safety.DecisionReview,
		Provider:            "test-provider",
		Model:               "moderator-v1",
		ReasonCode:          "NEEDS_REVIEW",
		ServiceCostMicroUSD: 11,
		VendorRequestID:     "vendor-mod-1",
	}}
	meter := &recordingSafetyMeter{}
	idSeq := atomic.Int64{}
	mod := &LLMModerator{
		Model:         model,
		PolicyVersion: "policy-v1",
		Clock:         func() time.Time { return time.Unix(300, 0).UTC() },
		NewID:         func() string { idSeq.Add(1); return "verdict-id" },
		UsageMeter:    meter,
	}
	verdict, err := mod.Moderate(context.Background(), ModerateInput{
		Layer:      safety.LayerInputModeration,
		TenantID:   "tenant-test",
		JobID:      "gen_mod",
		OutputType: generation.OutputImage,
		Model:      "downstream-model",
		Prompt:     "ordinary prompt",
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if model.last.Model != "downstream-model" {
		t.Fatalf("model input Model = %q, want downstream-model", model.last.Model)
	}
	if verdict.Decision != safety.DecisionReview || verdict.PolicyVersion != "policy-v1" {
		t.Fatalf("verdict = %+v, want REVIEW policy-v1", verdict)
	}
	if verdict.ID == "" {
		t.Fatal("verdict.ID empty — LLM-path verdicts must carry an ID")
	}
	if meter.calls.Load() != 1 || meter.cost.Load() != 11 || meter.source != ServiceCostSourceInputModeration || meter.requestID != "vendor-mod-1" {
		t.Fatalf("meter = calls=%d cost=%d source=%q request=%q, want 1/11/%s/vendor-mod-1", meter.calls.Load(), meter.cost.Load(), meter.source, meter.requestID, ServiceCostSourceInputModeration)
	}

	verdict, err = mod.Moderate(context.Background(), ModerateInput{
		Layer:  safety.LayerInputModeration,
		JobID:  "gen_block",
		Prompt: "bad " + SimulatedSentinel,
	})
	if err != nil {
		t.Fatalf("prefilter Moderate: %v", err)
	}
	if verdict.Decision != safety.DecisionFail || verdict.Provider != "prefilter" {
		t.Fatalf("prefilter verdict = %+v, want FAIL from prefilter", verdict)
	}
	if verdict.ID == "" || verdict.PolicyVersion != "policy-v1" || verdict.CreatedAt.IsZero() {
		t.Fatalf("prefilter verdict missing ID/PolicyVersion/CreatedAt: %+v", verdict)
	}
	if model.calls != 1 {
		t.Fatalf("model calls = %d, want 1 after prefilter short-circuit", model.calls)
	}
	if meter.calls.Load() != 1 {
		t.Fatalf("prefilter must not call meter (calls=%d)", meter.calls.Load())
	}
}

func TestLLMModeratorOutputLayerUsesOutputSource(t *testing.T) {
	model := &recordingModerationModel{out: ModerationModelResult{
		Decision:            safety.DecisionPass,
		Provider:            "test-provider",
		Model:               "moderator-v1",
		ServiceCostMicroUSD: 7,
	}}
	meter := &recordingSafetyMeter{}
	mod := &LLMModerator{Model: model, UsageMeter: meter}
	_, err := mod.Moderate(context.Background(), ModerateInput{
		Layer: safety.LayerOutputModeration,
		JobID: "gen_out",
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if meter.source != ServiceCostSourceOutputModeration {
		t.Fatalf("source = %q, want %q", meter.source, ServiceCostSourceOutputModeration)
	}
	if meter.requestID != "gen_out" {
		t.Fatalf("requestID fallback = %q, want gen_out", meter.requestID)
	}
}

func TestLLMModeratorScrubsModelErrors(t *testing.T) {
	model := &recordingModerationModel{err: errors.New("provider leaked prompt=secret vendor=request-id")}
	mod := &LLMModerator{Model: model}

	_, err := mod.Moderate(context.Background(), ModerateInput{
		Layer:  safety.LayerInputModeration,
		JobID:  "gen_mod",
		Prompt: "secret",
	})
	if !errors.Is(err, errLLMModerationProviderFailed) {
		t.Fatalf("err = %v, want sanitized provider failure", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "vendor") {
		t.Fatalf("sanitized error leaked provider detail: %q", err.Error())
	}
}

type recordingModerationModel struct {
	out   ModerationModelResult
	err   error
	last  ModerationModelRequest
	calls int
}

func (m *recordingModerationModel) Moderate(_ context.Context, req ModerationModelRequest) (ModerationModelResult, error) {
	m.calls++
	m.last = req
	return m.out, m.err
}

type recordingSafetyMeter struct {
	calls     atomic.Int64
	cost      atomic.Int64
	jobID     string
	source    string
	requestID string
}

func (m *recordingSafetyMeter) RecordServiceCost(_ context.Context, jobID, source, requestID string, microUSD int64) error {
	m.calls.Add(1)
	m.cost.Add(microUSD)
	m.jobID = jobID
	m.source = source
	m.requestID = requestID
	return nil
}
