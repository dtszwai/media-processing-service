package generation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// MemRepo.AdvanceStageAndEnqueue must reject a terminal-complete result that
// omits CompletedAt: resolveTransition's OutcomePublished branch always sets
// it, so a nil reaching the repo signals a wiring bug.
func TestMemRepo_AdvanceStageAndEnqueue_RejectsTerminalCompleteWithoutCompletedAt(t *testing.T) {
	repo := gen.NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	job := generation.Job{
		ID:           "gen_terminal_no_completed_at",
		TenantID:     "tenant-test",
		MediaID:      "med-test",
		OutputType:   generation.OutputImage,
		Tier:         generation.TierFree,
		Status:       generation.StatusRunning,
		CurrentStage: generation.StagePublish,
		StageVersion: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	persistedJob, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}

	err = repo.AdvanceStageAndEnqueue(ctx, persistedJob, gen.StageResult{
		NextStage: generation.StageTerminal,
		// TerminalError == nil and CompletedAt == nil — the rejected shape.
	})
	if err == nil {
		t.Fatalf("AdvanceStageAndEnqueue accepted terminal-complete without CompletedAt")
	}
	if !strings.Contains(err.Error(), "CompletedAt") {
		t.Fatalf("error %q should mention CompletedAt", err.Error())
	}

	// A terminal-complete result with CompletedAt set must still succeed.
	completed := now.Add(time.Second)
	if err := repo.AdvanceStageAndEnqueue(ctx, persistedJob, gen.StageResult{
		NextStage:   generation.StageTerminal,
		CompletedAt: &completed,
	}); err != nil {
		t.Fatalf("AdvanceStageAndEnqueue with CompletedAt: %v", err)
	}
}
