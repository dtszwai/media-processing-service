package generation

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// stageDelivery is the FSM's terminal-COMPLETE gateway. The artifact is
// already persisted (DISCLOSURE_POSTPROCESS), so this stage just stamps CompletedAt and
// hands off to AdvanceStageAndEnqueue which performs the atomic
// Job-status + Media-lifecycle flip.
func (w *Workflow) stageDelivery(_ context.Context, job *generation.Job) (StageResult, error) {
	if job.ResultAssetID == "" {
		return StageResult{}, generation.Terminal("PUBLISH_NO_ASSET", "no result asset id on job at delivery")
	}
	now := w.now()
	return StageResult{
		NextStage:   StageTerminal,
		CompletedAt: &now,
	}, nil
}
