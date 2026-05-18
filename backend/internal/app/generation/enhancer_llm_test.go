package generation

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestLLMEnhancerEmptyOutputMetersCostAndFailsClaim(t *testing.T) {
	ctx := context.Background()
	model := &recordingEnhancementModel{out: PromptEnhancementModelResult{
		Prompt:              "   ",
		Provider:            "test-llm",
		Model:               "model-v1",
		ServiceCostMicroUSD: 9,
		VendorRequestID:     "vendor-empty",
	}}
	repo := NewMemRepo()
	usage := &recordingEnhancementUsage{}
	enhancer := &LLMEnhancer{
		Model:         model,
		Store:         repo,
		Idempotency:   NewMemIdempotency(),
		Sealer:        preprocessSealer{},
		UsageMeter:    usage,
		PolicyVersion: "llm-test-v1",
		Clock:         func() time.Time { return time.Unix(200, 0).UTC() },
	}
	in := EnhanceInput{
		TenantID:     "tenant-test",
		JobID:        "gen_llm_empty",
		Prompt:       "raw prompt",
		OutputType:   domaingen.OutputImage,
		Provider:     "codex",
		Model:        "gpt-image",
		Resolution:   "1024x1024",
		VariantCount: 1,
	}

	out, err := enhancer.Enhance(ctx, in)
	if err != nil {
		t.Fatalf("first Enhance: %v", err)
	}
	if strings.TrimSpace(out.Prompt) != "" {
		t.Fatalf("output prompt = %q, want empty", out.Prompt)
	}
	if usage.serviceCost.Load() != 9 {
		t.Fatalf("service cost = %d, want 9 (cost incurred even on empty)", usage.serviceCost.Load())
	}
	if model.calls.Load() != 1 {
		t.Fatalf("model calls = %d, want 1", model.calls.Load())
	}

	// Retry must NOT re-invoke the model — the claim is failed permanently.
	_, err = enhancer.Enhance(ctx, in)
	if err == nil {
		t.Fatal("retry must return terminal error from failed claim")
	}
	if !domaingen.IsTerminal(err) {
		t.Fatalf("retry err = %v, want terminal", err)
	}
	if model.calls.Load() != 1 {
		t.Fatalf("model calls after retry = %d, want 1 (claim failed permanently)", model.calls.Load())
	}
	if usage.serviceCost.Load() != 9 {
		t.Fatalf("retry service cost = %d, want unchanged 9", usage.serviceCost.Load())
	}
}

func TestLLMEnhancerStoresEncryptedOutputAndReplaysByRef(t *testing.T) {
	ctx := context.Background()
	model := &recordingEnhancementModel{out: PromptEnhancementModelResult{
		Prompt:              "enhanced prompt",
		Provider:            "test-llm",
		Model:               "model-v1",
		TokensIn:            3,
		TokensOut:           5,
		ServiceCostMicroUSD: 7,
		VendorRequestID:     "vendor-1",
	}}
	repo := NewMemRepo()
	usage := &recordingEnhancementUsage{}
	enhancer := &LLMEnhancer{
		Model:         model,
		Store:         repo,
		Idempotency:   NewMemIdempotency(),
		Sealer:        preprocessSealer{},
		UsageMeter:    usage,
		PolicyVersion: "llm-test-v1",
		Clock:         func() time.Time { return time.Unix(200, 0).UTC() },
	}
	in := EnhanceInput{
		TenantID:     "tenant-test",
		JobID:        "gen_llm",
		Prompt:       "raw prompt",
		OutputType:   domaingen.OutputImage,
		Provider:     "codex",
		Model:        "gpt-image",
		Resolution:   "1024x1024",
		VariantCount: 1,
	}

	first, err := enhancer.Enhance(ctx, in)
	if err != nil {
		t.Fatalf("first Enhance: %v", err)
	}
	if first.Prompt != "enhanced prompt" || !first.Applied || first.Ref == "" {
		t.Fatalf("first output = %+v, want enhanced prompt with ref", first)
	}
	if model.calls.Load() != 1 {
		t.Fatalf("model calls after first = %d, want 1", model.calls.Load())
	}
	if usage.serviceCost.Load() != 7 || usage.source != ServiceCostSourcePromptEnhance || usage.requestID != first.Ref {
		t.Fatalf("usage = cost %d source %q request %q, want 7/%s/%s", usage.serviceCost.Load(), usage.source, usage.requestID, ServiceCostSourcePromptEnhance, first.Ref)
	}

	rec, err := repo.GetPromptEnhancement(ctx, in.TenantID, in.JobID, first.Ref)
	if err != nil {
		t.Fatalf("GetPromptEnhancement: %v", err)
	}
	if string(rec.EncryptedPrompt) == "enhanced prompt" {
		t.Fatal("enhancement row stored plaintext prompt")
	}

	second, err := enhancer.Enhance(ctx, in)
	if err != nil {
		t.Fatalf("second Enhance: %v", err)
	}
	if second.Prompt != first.Prompt || second.Ref != first.Ref {
		t.Fatalf("second output = %+v, want replay of first %+v", second, first)
	}
	if model.calls.Load() != 1 {
		t.Fatalf("model calls after replay = %d, want 1", model.calls.Load())
	}
	if usage.serviceCost.Load() != 7 {
		t.Fatalf("service cost after replay = %d, want unchanged 7", usage.serviceCost.Load())
	}
}

type recordingEnhancementModel struct {
	out   PromptEnhancementModelResult
	calls atomic.Int64
}

func (m *recordingEnhancementModel) EnhancePrompt(context.Context, PromptEnhancementModelRequest) (PromptEnhancementModelResult, error) {
	m.calls.Add(1)
	return m.out, nil
}

type recordingEnhancementUsage struct {
	serviceCost atomic.Int64
	source      string
	requestID   string
}

func (u *recordingEnhancementUsage) RecordGeneratedOutput(context.Context, string, string, string) error {
	return nil
}

func (u *recordingEnhancementUsage) RecordVendorCost(context.Context, string, string, int64) error {
	return nil
}

func (u *recordingEnhancementUsage) RecordServiceCost(_ context.Context, _ string, source, requestID string, microUSD int64) error {
	u.source = source
	u.requestID = requestID
	u.serviceCost.Add(microUSD)
	return nil
}
