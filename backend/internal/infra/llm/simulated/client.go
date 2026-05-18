// Package simulated provides a deterministic in-process LLM client for local
// tests and compose smoke paths.
package simulated

import (
	"context"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/llm"
)

type Client struct{}

func (Client) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	user := userText(req.Messages)
	text := user

	switch req.Metadata["feature"] {
	case "prompt_enhancement":
		prefix := "Describe the scene with concrete visual detail."
		if req.Metadata["output_type"] == "AUDIO" {
			prefix = "Prepare narration notes with a clear audio structure."
		}
		if strings.TrimSpace(user) != "" {
			text = prefix + "\n\n" + strings.TrimSpace(user)
		}
	case "safety_moderation":
		text = "PASS"
		if strings.Contains(user, "__moderation_block__") {
			text = "FAIL"
		}
		if strings.Contains(user, "__moderation_review__") {
			text = "REVIEW"
		}
	}

	model := req.Model
	if model == "" {
		model = "simulated-llm-v1"
	}
	return llm.CompletionResponse{
		Text:         text,
		Provider:     "simulated",
		Model:        model,
		TokensIn:     int64(len(strings.Fields(user))),
		TokensOut:    int64(len(strings.Fields(text))),
		CostMicroUSD: 1,
		RequestID:    "simulated",
	}, nil
}

func userText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}
