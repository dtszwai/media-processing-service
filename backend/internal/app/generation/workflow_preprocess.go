package generation

import (
	"context"
	"fmt"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

const promptSpecVersion = "prompt-policy-v1"

// stagePreprocess is the PROMPT_PREPARE stage. It produces the exact prompt
// the provider will receive, seals it, persists its stable hash, and hands off
// to PROVIDER_SUBMIT. Provider idempotency is computed from this prepared hash
// so retries bind to the actual provider input rather than the raw request.
func (w *Workflow) stagePreprocess(ctx context.Context, job *generation.Job) (StageResult, error) {
	prepared := job.Prompt
	if strings.TrimSpace(prepared) == "" {
		return StageResult{}, generation.Terminal("EMPTY_PROMPT", "prompt is empty")
	}
	paramsHash := generationParamsHash(job)
	preparedHash := idempotency.HashInputs(
		promptSpecVersion,
		job.TenantID,
		job.ID,
		string(job.OutputType),
		job.Provider,
		job.Model,
		prepared,
		paramsHash,
	)
	var sealed []byte
	if w.PromptSealer != nil {
		var err error
		sealed, err = w.PromptSealer.Seal(ctx, job.TenantID, job.ID, prepared)
		if err != nil {
			return StageResult{}, fmt.Errorf("prompt prepare: seal: %w", err)
		}
	}
	result := w.nextStageResult(ctx, job, generation.StageProviderSubmit, generation.ResourceProvider)
	result.PreparedPrompt = prepared
	result.EncryptedPreparedPrompt = sealed
	result.PreparedPromptHash = preparedHash
	result.PromptSpecVersion = promptSpecVersion
	result.GenerationParamsHash = paramsHash
	return result, nil
}

func generationParamsHash(job *generation.Job) string {
	return idempotency.HashInputs(
		"generation-params-v1",
		string(job.OutputType),
		job.Provider,
		job.Model,
		job.Resolution,
		fmt.Sprintf("%d", job.Seed),
		fmt.Sprintf("%d", job.VariantCount),
	)
}
