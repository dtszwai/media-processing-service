package generation

import (
	"context"
	"errors"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/llm"
)

// Policy + response interpretation stay here; the llm.Client owns transport.
type LLMPromptEnhancementModel struct {
	Client llm.Client
	Model  string
}

func (m LLMPromptEnhancementModel) EnhancePrompt(ctx context.Context, req PromptEnhancementModelRequest) (PromptEnhancementModelResult, error) {
	if m.Client == nil {
		return PromptEnhancementModelResult{}, errors.New("prompt enhancement model: llm client required")
	}
	if m.Model == "" {
		return PromptEnhancementModelResult{}, errors.New("prompt enhancement model: model required")
	}
	resp, err := m.Client.Complete(ctx, llm.CompletionRequest{
		Model: m.Model,
		Messages: []llm.Message{
			{
				Role:    llm.RoleSystem,
				Content: "Enhance the user prompt for downstream media generation. Return only the enhanced prompt.",
			},
			{
				Role:    llm.RoleUser,
				Content: strings.TrimSpace(req.Prompt),
			},
		},
		Metadata: map[string]string{
			"feature":     "prompt_enhancement",
			"output_type": string(req.OutputType),
			"provider":    req.Provider,
			"model":       req.Model,
			"resolution":  req.Resolution,
		},
	})
	if err != nil {
		return PromptEnhancementModelResult{}, err
	}
	return PromptEnhancementModelResult{
		Prompt:              resp.Text,
		Provider:            resp.Provider,
		Model:               resp.Model,
		TokensIn:            resp.TokensIn,
		TokensOut:           resp.TokensOut,
		ServiceCostMicroUSD: resp.CostMicroUSD,
		VendorRequestID:     resp.RequestID,
	}, nil
}
