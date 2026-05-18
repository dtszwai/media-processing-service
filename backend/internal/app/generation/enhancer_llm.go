package generation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

const (
	defaultLLMEnhancerPolicyVersion = "llm-enhance-v1"
	defaultPromptEnhancementTTL     = 30 * 24 * time.Hour
)

type LLMEnhancer struct {
	Model         PromptEnhancementModel
	Store         PromptEnhancementStore
	Idempotency   idempotency.Store
	Sealer        PromptSealer
	UsageMeter    UsageMeter
	PolicyVersion string
	Lease         time.Duration
	TTL           time.Duration
	Clock         func() time.Time
}

func (e *LLMEnhancer) Enhance(ctx context.Context, in EnhanceInput) (EnhanceOutput, error) {
	if e == nil {
		return EnhanceOutput{}, errors.New("prompt enhancer: nil LLMEnhancer")
	}
	if e.Model == nil {
		return EnhanceOutput{}, errors.New("prompt enhancer: model required")
	}
	if e.Store == nil {
		return EnhanceOutput{}, errors.New("prompt enhancer: store required")
	}
	if e.Sealer == nil {
		return EnhanceOutput{}, errors.New("prompt enhancer: sealer required")
	}
	policy := e.policyVersion()
	rawPromptHash := idempotency.HashInputs("raw-prompt-v1", in.Prompt)
	ref := PromptEnhancementRef(policy, in, rawPromptHash)
	scope := genScope(in.JobID, "PROMPT_ENHANCEMENT#"+ref)
	inputHash := PromptEnhancementInputHash(policy, in)

	token := ""
	if e.Idempotency != nil {
		acquired, err := idempotency.Acquire(ctx, e.Idempotency, scope, inputHash, e.lease())
		if err != nil {
			return EnhanceOutput{}, fmt.Errorf("prompt enhancement: %w", err)
		}
		switch acquired.Kind {
		case idempotency.AcquireCompleted:
			cachedRef := acquired.CachedRef
			if cachedRef == "" {
				cachedRef = ref
			}
			return e.replay(ctx, in, cachedRef)
		case idempotency.AcquireOwned, idempotency.AcquireReclaimed:
			token = acquired.Token
		case idempotency.AcquireInFlight:
			return EnhanceOutput{}, generation.Transient("PROMPT_ENHANCEMENT_IN_FLIGHT", "another worker holds the prompt enhancement claim")
		case idempotency.AcquirePermanentlyFailed:
			return EnhanceOutput{}, generation.Terminal("PROMPT_ENHANCEMENT_PERMANENT_FAILURE", "prompt enhancement claim already failed")
		case idempotency.AcquireInputConflict:
			return EnhanceOutput{}, generation.Terminal("PROMPT_ENHANCEMENT_INPUT_CONFLICT", "input hash conflict for stable prompt enhancement claim")
		}
	}

	if rec, found, err := e.loadExisting(ctx, in, ref); err != nil {
		e.abandon(ctx, scope, token)
		return EnhanceOutput{}, err
	} else if found {
		return e.completeFromRecord(ctx, in, scope, token, rec)
	}

	modelOut, err := e.Model.EnhancePrompt(ctx, PromptEnhancementModelRequest{
		PolicyVersion: policy,
		OutputType:    in.OutputType,
		Provider:      in.Provider,
		Model:         in.Model,
		Resolution:    in.Resolution,
		VariantCount:  in.VariantCount,
		Prompt:        in.Prompt,
	})
	if err != nil {
		e.abandon(ctx, scope, token)
		return EnhanceOutput{}, err
	}
	if strings.TrimSpace(modelOut.Prompt) == "" {
		// Fail the claim so any retry surfaces AcquirePermanentlyFailed
		// rather than re-invoking the model on cost the LLM already burned.
		e.meterCost(ctx, in.JobID, ref, modelOut.ServiceCostMicroUSD)
		e.failClaim(ctx, scope, token, "PROMPT_ENHANCEMENT_EMPTY_OUTPUT")
		return EnhanceOutput{
			Prompt:        modelOut.Prompt,
			PolicyVersion: policy,
			Provider:      modelOut.Provider,
			Model:         modelOut.Model,
			TokensIn:      modelOut.TokensIn,
			TokensOut:     modelOut.TokensOut,
			Ref:           ref,
		}, nil
	}

	sealed, err := e.Sealer.Seal(ctx, in.TenantID, in.JobID, modelOut.Prompt)
	if err != nil {
		e.abandon(ctx, scope, token)
		return EnhanceOutput{}, fmt.Errorf("prompt enhancement: seal output: %w", err)
	}
	rec := PromptEnhancementRecord{
		Ref:                 ref,
		TenantID:            in.TenantID,
		JobID:               in.JobID,
		OutputType:          in.OutputType,
		EncryptedPrompt:     sealed,
		RawPromptHash:       rawPromptHash,
		PolicyVersion:       policy,
		Provider:            modelOut.Provider,
		Model:               modelOut.Model,
		DownstreamProvider:  in.Provider,
		DownstreamModel:     in.Model,
		Resolution:          in.Resolution,
		VariantCount:        in.VariantCount,
		TokensIn:            modelOut.TokensIn,
		TokensOut:           modelOut.TokensOut,
		ServiceCostMicroUSD: modelOut.ServiceCostMicroUSD,
		VendorRequestID:     modelOut.VendorRequestID,
		CreatedAt:           e.now(),
	}
	rec.TTLEpoch = rec.CreatedAt.Add(e.ttl()).Unix()
	if err := e.Store.PutPromptEnhancement(ctx, rec); err != nil {
		e.abandon(ctx, scope, token)
		return EnhanceOutput{}, fmt.Errorf("prompt enhancement: store output: %w", err)
	}
	return e.completeFromRecord(ctx, in, scope, token, rec)
}

// PromptEnhancementInputHash binds the claim to the exact prompt + downstream
// params so a different prompt re-claims rather than replaying.
func PromptEnhancementInputHash(policy string, in EnhanceInput) string {
	return promptEnhancementHash(policy, in, in.Prompt)
}

// PromptEnhancementRef is seeded from rawPromptHash, not the raw prompt, so
// the public reference (job row, audit, stage messages) carries no plaintext.
func PromptEnhancementRef(policy string, in EnhanceInput, rawPromptHash string) string {
	return "enh_" + promptEnhancementHash(policy, in, rawPromptHash)[:24]
}

func promptEnhancementHash(policy string, in EnhanceInput, tail string) string {
	return idempotency.HashInputs(
		policy,
		string(in.OutputType),
		in.Provider,
		in.Model,
		in.Resolution,
		strconv.Itoa(in.VariantCount),
		tail,
	)
}

func (e *LLMEnhancer) replay(ctx context.Context, in EnhanceInput, ref string) (EnhanceOutput, error) {
	rec, err := e.Store.GetPromptEnhancement(ctx, in.TenantID, in.JobID, ref)
	if err != nil {
		return EnhanceOutput{}, fmt.Errorf("prompt enhancement: replay %s: %w", ref, err)
	}
	return e.outputFromRecord(ctx, in, rec)
}

func (e *LLMEnhancer) completeFromRecord(ctx context.Context, in EnhanceInput, scope, token string, rec PromptEnhancementRecord) (EnhanceOutput, error) {
	// Complete before metering: a failure to flip COMPLETED would otherwise
	// leave a recorded cost against a still-CLAIMED row and re-meter on reclaim.
	if e.Idempotency != nil && token != "" {
		if err := e.Idempotency.Complete(ctx, scope, token, rec.Ref); err != nil {
			return EnhanceOutput{}, fmt.Errorf("prompt enhancement: complete claim: %w", err)
		}
	}
	e.meterCost(ctx, in.JobID, rec.Ref, rec.ServiceCostMicroUSD)
	return e.outputFromRecord(ctx, in, rec)
}

func (e *LLMEnhancer) outputFromRecord(ctx context.Context, in EnhanceInput, rec PromptEnhancementRecord) (EnhanceOutput, error) {
	prompt, err := e.Sealer.Unseal(ctx, in.TenantID, in.JobID, rec.EncryptedPrompt)
	if err != nil {
		return EnhanceOutput{}, fmt.Errorf("prompt enhancement: unseal output: %w", err)
	}
	return EnhanceOutput{
		Prompt:              prompt,
		Applied:             prompt != in.Prompt,
		PolicyVersion:       rec.PolicyVersion,
		Provider:            rec.Provider,
		Model:               rec.Model,
		TokensIn:            rec.TokensIn,
		TokensOut:           rec.TokensOut,
		ServiceCostMicroUSD: rec.ServiceCostMicroUSD,
		Ref:                 rec.Ref,
	}, nil
}

func (e *LLMEnhancer) loadExisting(ctx context.Context, in EnhanceInput, ref string) (PromptEnhancementRecord, bool, error) {
	rec, err := e.Store.GetPromptEnhancement(ctx, in.TenantID, in.JobID, ref)
	if err == nil {
		return rec, true, nil
	}
	if errors.Is(err, kv.ErrNotFound) {
		return PromptEnhancementRecord{}, false, nil
	}
	return PromptEnhancementRecord{}, false, fmt.Errorf("prompt enhancement: load existing %s: %w", ref, err)
}

func (e *LLMEnhancer) meterCost(ctx context.Context, jobID, ref string, microUSD int64) {
	if e.UsageMeter == nil || microUSD <= 0 {
		return
	}
	_ = e.UsageMeter.RecordServiceCost(ctx, jobID, ServiceCostSourcePromptEnhance, ref, microUSD)
}

func (e *LLMEnhancer) abandon(ctx context.Context, scope, token string) {
	if e.Idempotency == nil || token == "" {
		return
	}
	_ = e.Idempotency.Abandon(ctx, scope, token)
}

func (e *LLMEnhancer) failClaim(ctx context.Context, scope, token, code string) {
	if e.Idempotency == nil || token == "" {
		return
	}
	_ = e.Idempotency.Fail(ctx, scope, token, code)
}

func (e *LLMEnhancer) policyVersion() string {
	if e.PolicyVersion != "" {
		return e.PolicyVersion
	}
	return defaultLLMEnhancerPolicyVersion
}

func (e *LLMEnhancer) lease() time.Duration {
	if e.Lease > 0 {
		return e.Lease
	}
	return 5 * time.Minute
}

func (e *LLMEnhancer) ttl() time.Duration {
	if e.TTL > 0 {
		return e.TTL
	}
	return defaultPromptEnhancementTTL
}

func (e *LLMEnhancer) now() time.Time {
	if e.Clock != nil {
		return e.Clock().UTC()
	}
	return time.Now().UTC()
}
