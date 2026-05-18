package generation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

const basePromptSpecVersion = "prompt-policy-v1"

// stagePreprocess is the PROMPT_PREPARE stage. It produces the exact prompt
// the provider will receive, seals it, persists its stable hash, and hands off
// to PROVIDER_SUBMIT. Provider idempotency is computed from this prepared hash
// so retries bind to the actual provider input rather than the raw request.
func (w *Workflow) stagePreprocess(ctx context.Context, job *generation.Job) (StageResult, error) {
	prepared := job.Prompt
	if strings.TrimSpace(prepared) == "" {
		return StageResult{}, generation.Terminal("EMPTY_PROMPT", "prompt is empty")
	}
	enhancerPolicy := "none"
	outcome := "passthrough"
	var enhanceOut EnhanceOutput
	enhanced := false
	if w.PromptEnhancer != nil {
		out, err := w.PromptEnhancer.Enhance(ctx, EnhanceInput{
			TenantID:     job.TenantID,
			JobID:        job.ID,
			Prompt:       prepared,
			OutputType:   job.OutputType,
			Provider:     job.Provider,
			Model:        job.Model,
			Resolution:   job.Resolution,
			VariantCount: job.VariantCount,
		})
		if err != nil {
			w.emitPromptEnhancementAttempt(ctx, "error", string(job.OutputType), "unknown")
			var genErr *generation.Error
			if errors.As(err, &genErr) {
				return StageResult{}, err
			}
			// Don't echo the LLM SDK message into the persisted stage-attempt
			// row — vendor error strings sometimes carry request IDs,
			// endpoints, or prompt fragments. Log it for operator debugging
			// and surface a constant-shaped transient code instead.
			slog.WarnContext(ctx, "prompt enhancer provider error",
				"job_id", job.ID,
				"output_type", string(job.OutputType),
				"err", err,
			)
			return StageResult{}, generation.Transient("PROMPT_ENHANCEMENT_PROVIDER_ERROR", "prompt enhancer provider call failed")
		}
		enhanceOut = out
		prepared = out.Prompt
		enhancerPolicy = out.PolicyVersion
		if enhancerPolicy == "" {
			enhancerPolicy = "unknown"
		}
		enhanced = true
		if out.Applied {
			outcome = "applied"
		}
	}
	if strings.TrimSpace(prepared) == "" {
		if enhanced {
			w.emitPromptEnhancementAttempt(ctx, "empty", string(job.OutputType), enhancerPolicy)
		}
		return StageResult{}, generation.Terminal("EMPTY_PROMPT_AFTER_ENHANCEMENT", "enhancer returned empty prompt")
	}
	if enhanced {
		w.emitPromptEnhancementAttempt(ctx, outcome, string(job.OutputType), enhancerPolicy)
	}
	paramsHash := generationParamsHash(job)
	promptSpec := basePromptSpecVersion + "+enhancer-" + enhancerPolicy
	preparedHash := idempotency.HashInputs(
		promptSpec,
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
	result := StageResult{Outcome: OutcomePromptPrepared}
	result.PreparedPrompt = prepared
	result.EncryptedPreparedPrompt = sealed
	result.PreparedPromptHash = preparedHash
	result.PromptSpecVersion = promptSpec
	result.GenerationParamsHash = paramsHash
	if enhanced {
		applied := enhanceOut.Applied
		result.PromptEnhancementApplied = &applied
		result.PromptEnhancementRef = enhanceOut.Ref
		result.AuditEvents = append(result.AuditEvents, auditapp.NewWorkflowPromptEnhancementApplied(
			job.TenantID,
			job.ID,
			enhanceOut.Applied,
			enhanceOut.Ref,
			enhancerPolicy,
			enhanceOut.Provider,
			enhanceOut.Model,
			string(job.OutputType),
			enhanceOut.TokensIn,
			enhanceOut.TokensOut,
		))
	}
	return result, nil
}

func (w *Workflow) emitPromptEnhancementAttempt(ctx context.Context, outcome, outputType, policyVersion string) {
	w.Instruments.PromptEnhancementAttempts.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("output_type", outputType),
		attribute.String("policy_version", policyVersion),
	))
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
