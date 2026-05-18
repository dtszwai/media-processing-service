package bootstrap

import (
	"fmt"
	"strings"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	quotaapp "github.com/dtszwai/media-processing-service/backend/internal/app/quota"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	llmsim "github.com/dtszwai/media-processing-service/backend/internal/infra/llm/simulated"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/sealer"
)

func constructPromptEnhancer(cfg app.GenerationConfig, store genapp.PromptEnhancementStore, idem idempotency.Store, promptSealer sealer.Sealer, meter *quotaapp.Meter) (genapp.PromptEnhancer, error) {
	switch normalizeProviderName(cfg.PromptEnhancer.Provider) {
	case "", "passthrough":
		return &genapp.PassthroughEnhancer{}, nil
	case "simulated":
		return &genapp.LLMEnhancer{
			Model:         genapp.LLMPromptEnhancementModel{Client: llmsim.Client{}, Model: "simulated-enhancer-v1"},
			Store:         store,
			Idempotency:   idem,
			Sealer:        promptSealer,
			UsageMeter:    meter,
			PolicyVersion: strings.TrimSpace(cfg.PromptEnhancer.PolicyVersion),
			TTL:           time.Duration(cfg.PromptEnhancer.TTLDays) * 24 * time.Hour,
		}, nil
	default:
		return nil, fmt.Errorf("bootstrap: unknown prompt enhancer provider %q", cfg.PromptEnhancer.Provider)
	}
}

func constructModerator(cfg app.SafetyConfig, meter *quotaapp.Meter) (safetyapp.Moderator, error) {
	switch normalizeProviderName(cfg.Moderator.Provider) {
	case "", "simulated":
		m := safetyapp.NewSimulatedModerator()
		if v := strings.TrimSpace(cfg.Moderator.PolicyVersion); v != "" {
			m.PolicyVersion = v
		}
		return m, nil
	case "simulated-llm":
		return &safetyapp.LLMModerator{
			Model:         safetyapp.LLMModerationModel{Client: llmsim.Client{}, Model: "simulated-moderation-v1"},
			PolicyVersion: strings.TrimSpace(cfg.Moderator.PolicyVersion),
			UsageMeter:    meter,
		}, nil
	default:
		return nil, fmt.Errorf("bootstrap: unknown safety moderator provider %q", cfg.Moderator.Provider)
	}
}
