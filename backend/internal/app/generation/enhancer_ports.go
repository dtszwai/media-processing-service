package generation

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// PromptEnhancer prepares the provider-facing prompt for a generation job.
// Implementations may be pure functions (passthrough/prefix templates) or
// model-backed, but they return the same durable policy/provenance envelope so
// PROMPT_PREPARE can hash, audit, and meter the exact prompt sent downstream.
type PromptEnhancer interface {
	Enhance(ctx context.Context, in EnhanceInput) (EnhanceOutput, error)
}

type EnhanceInput struct {
	TenantID     string
	JobID        string
	Prompt       string
	OutputType   generation.OutputType
	Provider     string
	Model        string
	Resolution   string
	VariantCount int
}

// EnhanceOutput is the per-call result handed back to the workflow.
//
// Applied means "the enhancer changed the prompt text"; it is NOT a marker
// for "the model was invoked". An LLM-backed enhancer that echoes its input
// verbatim returns Applied=false even though it billed the call. Use
// Provider/Model/ServiceCostMicroUSD when you need invocation-level signal.
type EnhanceOutput struct {
	Prompt              string
	Applied             bool
	PolicyVersion       string
	Provider            string
	Model               string
	TokensIn            int64
	TokensOut           int64
	ServiceCostMicroUSD int64
	Ref                 string
}

// PromptEnhancementModel is the model-facing seam used by LLMEnhancer. It is
// deliberately narrower than a generic text-completion client so prompt
// construction and schema handling stay owned by the prompt-enhancement
// adapter rather than leaking across app packages.
type PromptEnhancementModel interface {
	EnhancePrompt(ctx context.Context, req PromptEnhancementModelRequest) (PromptEnhancementModelResult, error)
}

type PromptEnhancementModelRequest struct {
	PolicyVersion string
	OutputType    generation.OutputType
	Provider      string
	Model         string
	Resolution    string
	VariantCount  int
	Prompt        string
}

type PromptEnhancementModelResult struct {
	Prompt              string
	Provider            string
	Model               string
	TokensIn            int64
	TokensOut           int64
	ServiceCostMicroUSD int64
	VendorRequestID     string
}

type PromptEnhancementRecord struct {
	Ref                 string
	TenantID            string
	JobID               string
	OutputType          generation.OutputType
	EncryptedPrompt     []byte
	RawPromptHash       string
	PolicyVersion       string
	Provider            string
	Model               string
	DownstreamProvider  string
	DownstreamModel     string
	Resolution          string
	VariantCount        int
	TokensIn            int64
	TokensOut           int64
	ServiceCostMicroUSD int64
	VendorRequestID     string
	CreatedAt           time.Time
	TTLEpoch            int64
}

// PromptEnhancementStore persists encrypted enhancement outputs so a retry
// after a completed LLM side effect can reconstruct PreparedPrompt without
// storing raw prompt text in the idempotency row.
type PromptEnhancementStore interface {
	PutPromptEnhancement(ctx context.Context, rec PromptEnhancementRecord) error
	GetPromptEnhancement(ctx context.Context, tenantID, jobID, ref string) (PromptEnhancementRecord, error)
}
