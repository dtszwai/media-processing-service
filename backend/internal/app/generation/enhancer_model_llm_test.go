package generation

import (
	"context"
	"strings"
	"testing"

	llmsim "github.com/dtszwai/media-processing-service/backend/internal/infra/llm/simulated"
)

func TestLLMPromptEnhancementModelUsesLLMClient(t *testing.T) {
	model := LLMPromptEnhancementModel{Client: llmsim.Client{}, Model: "simulated-enhancer-v1"}

	out, err := model.EnhancePrompt(context.Background(), PromptEnhancementModelRequest{
		OutputType: "IMAGE",
		Provider:   "simulated",
		Model:      "simulated-v1",
		Prompt:     "a ceramic cup on a table",
	})
	if err != nil {
		t.Fatalf("EnhancePrompt: %v", err)
	}
	if out.Provider != "simulated" || out.Model != "simulated-enhancer-v1" {
		t.Fatalf("provider/model = %s/%s, want simulated/simulated-enhancer-v1", out.Provider, out.Model)
	}
	if !strings.Contains(out.Prompt, "concrete visual detail") || !strings.Contains(out.Prompt, "a ceramic cup") {
		t.Fatalf("enhanced prompt = %q", out.Prompt)
	}
	if out.ServiceCostMicroUSD != 1 || out.VendorRequestID == "" {
		t.Fatalf("cost/request = %d/%q, want 1/non-empty", out.ServiceCostMicroUSD, out.VendorRequestID)
	}
}
