package safety

import (
	"context"
	"errors"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/llm"
	llmsim "github.com/dtszwai/media-processing-service/backend/internal/infra/llm/simulated"
)

func TestLLMModerationModelUsesLLMClient(t *testing.T) {
	model := LLMModerationModel{Client: llmsim.Client{}, Model: "simulated-moderation-v1"}

	out, err := model.Moderate(context.Background(), ModerationModelRequest{
		Prompt: "please inspect " + SimulatedReviewSentinel,
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if out.Decision != safety.DecisionReview {
		t.Fatalf("decision = %s, want REVIEW", out.Decision)
	}
	if out.Provider != "simulated" || out.Model != "simulated-moderation-v1" {
		t.Fatalf("provider/model = %s/%s, want simulated/simulated-moderation-v1", out.Provider, out.Model)
	}
	if out.ReasonCode != "MODERATION_LLM_REVIEW" {
		t.Fatalf("reason = %q, want MODERATION_LLM_REVIEW", out.ReasonCode)
	}
	if out.ServiceCostMicroUSD <= 0 || out.VendorRequestID == "" {
		t.Fatalf("cost/requestID = %d/%q, want non-zero/non-empty", out.ServiceCostMicroUSD, out.VendorRequestID)
	}
}

func TestLLMModerationModelRequiresModel(t *testing.T) {
	model := LLMModerationModel{Client: llmsim.Client{}}
	if _, err := model.Moderate(context.Background(), ModerationModelRequest{Prompt: "anything"}); err == nil {
		t.Fatal("expected error when Model is empty")
	}
}

func TestLLMModerationModelReviewsUnparseableOutput(t *testing.T) {
	for _, text := range []string{"FAIL: policy violation", "BLOCK", ""} {
		model := LLMModerationModel{
			Client: fixedLLMClient{resp: llm.CompletionResponse{
				Text:     text,
				Provider: "test-provider",
				Model:    "moderator-v1",
			}},
			Model: "moderator-v1",
		}
		out, err := model.Moderate(context.Background(), ModerationModelRequest{Prompt: "anything"})
		if err != nil {
			t.Fatalf("Moderate(%q): %v", text, err)
		}
		if out.Decision != safety.DecisionReview {
			t.Fatalf("Moderate(%q) decision = %s, want REVIEW", text, out.Decision)
		}
		if out.ReasonCode != "MODERATION_LLM_UNPARSEABLE" {
			t.Fatalf("Moderate(%q) reason = %q, want MODERATION_LLM_UNPARSEABLE", text, out.ReasonCode)
		}
	}
}

func TestLLMModerationModelScrubsProviderErrors(t *testing.T) {
	model := LLMModerationModel{
		Client: fixedLLMClient{err: errors.New("provider leaked prompt=secret vendor=request-id")},
		Model:  "moderator-v1",
	}
	if _, err := model.Moderate(context.Background(), ModerationModelRequest{Prompt: "secret"}); !errors.Is(err, errLLMModerationProviderFailed) {
		t.Fatalf("err = %v, want sanitized provider failure", err)
	}
}

type fixedLLMClient struct {
	resp llm.CompletionResponse
	err  error
}

func (c fixedLLMClient) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return c.resp, c.err
}
