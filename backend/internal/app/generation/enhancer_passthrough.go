package generation

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

const passthroughEnhancerPolicyVersion = "passthrough-v0"

type PassthroughEnhancer struct {
	Prefixes map[generation.OutputType]string
}

func (e *PassthroughEnhancer) Enhance(_ context.Context, in EnhanceInput) (EnhanceOutput, error) {
	prefix := ""
	if e != nil && e.Prefixes != nil {
		prefix = e.Prefixes[in.OutputType]
	}
	out := in.Prompt
	applied := false
	if prefix != "" {
		out = prefix + "\n\n" + in.Prompt
		applied = true
	}
	return EnhanceOutput{
		Prompt:        out,
		Applied:       applied,
		PolicyVersion: passthroughEnhancerPolicyVersion,
		Provider:      "passthrough",
	}, nil
}
