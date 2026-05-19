package safety

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

// Exactly one of Prompt or Artifact is set: INPUT_MODERATION sets Prompt,
// OUTPUT_MODERATION sets Artifact.
type ModerateInput struct {
	Layer      safety.Layer
	TenantID   string
	JobID      string
	OutputType generation.OutputType
	Model      string
	Prompt     string
	Artifact   *generation.Artifact
}

type Moderator interface {
	Moderate(ctx context.Context, in ModerateInput) (safety.Verdict, error)
}

// ServiceCostMeter is satisfied structurally by quota.Meter.
type ServiceCostMeter interface {
	RecordServiceCost(ctx context.Context, jobID, source, requestID string, microUSD int64) error
}

const (
	ServiceCostSourceInputModeration  = "input-moderation"
	ServiceCostSourceOutputModeration = "output-moderation"
)
