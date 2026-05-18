// Package llm defines the shared contract for text-oriented LLM providers.
// Feature packages wrap this low-level seam with app-owned interpretation.
package llm

import "context"

type Role string

const (
	RoleSystem Role = "system"
	RoleUser   Role = "user"
)

type Message struct {
	Role    Role
	Content string
}

type CompletionRequest struct {
	Model    string
	Messages []Message
	Metadata map[string]string
}

type CompletionResponse struct {
	Text         string
	Provider     string
	Model        string
	TokensIn     int64
	TokensOut    int64
	CostMicroUSD int64
	RequestID    string
}

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
