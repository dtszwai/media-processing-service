package safety

import (
	"context"
	"errors"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/llm"
)

// Policy + response interpretation stay here; the llm.Client owns transport.
type LLMModerationModel struct {
	Client llm.Client
	Model  string
}

func (m LLMModerationModel) Moderate(ctx context.Context, req ModerationModelRequest) (ModerationModelResult, error) {
	if m.Client == nil {
		return ModerationModelResult{}, errors.New("moderation model: llm client required")
	}
	if m.Model == "" {
		return ModerationModelResult{}, errors.New("moderation model: model required")
	}
	resp, err := m.Client.Complete(ctx, llm.CompletionRequest{
		Model: m.Model,
		Messages: []llm.Message{
			{
				Role:    llm.RoleSystem,
				Content: "Moderate the user content. Return only PASS, FAIL, or REVIEW.",
			},
			{
				Role:    llm.RoleUser,
				Content: moderationLLMUserText(req),
			},
		},
		Metadata: map[string]string{
			"feature":     "safety_moderation",
			"layer":       string(req.Layer),
			"output_type": string(req.OutputType),
			"model":       req.Model,
		},
	})
	if err != nil {
		return ModerationModelResult{}, errLLMModerationProviderFailed
	}
	decision, reason := parseModerationDecision(resp.Text)
	return ModerationModelResult{
		Decision:            decision,
		ReasonCode:          reason,
		Provider:            resp.Provider,
		Model:               resp.Model,
		ServiceCostMicroUSD: resp.CostMicroUSD,
		VendorRequestID:     resp.RequestID,
	}, nil
}

func moderationLLMUserText(req ModerationModelRequest) string {
	if req.Artifact == nil {
		return req.Prompt
	}
	parts := make([]string, 0, len(req.Artifact.Metadata)+1)
	parts = append(parts, req.Prompt)
	for k, v := range req.Artifact.Metadata {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "\n")
}

func parseModerationDecision(text string) (safety.Decision, string) {
	switch strings.ToUpper(strings.TrimSpace(text)) {
	case string(safety.DecisionFail):
		return safety.DecisionFail, "MODERATION_LLM_BLOCK"
	case string(safety.DecisionReview):
		return safety.DecisionReview, "MODERATION_LLM_REVIEW"
	default:
		return safety.DecisionReview, "MODERATION_LLM_UNPARSEABLE"
	}
}
