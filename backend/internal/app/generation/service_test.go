package generation_test

import (
	"context"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

// TestWorkflow_RunCompletes is the integration smoke test for the FSM:
// seed a queued job, run the workflow, and verify it transitions to
// COMPLETE with the configured result asset id.
func TestWorkflow_RunCompletes(t *testing.T) {
	repo := gen.NewMemRepo()
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-svc-1"
	wf := newTestWorkflow(t, repo, simulated.New(), gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	now := time.Now().UTC()
	job := generation.Job{
		ID:           "gen_test_svc",
		TenantID:     "tenant-svc",
		MediaID:      "med-svc",
		OutputType:   generation.OutputImage,
		Tier:         generation.TierFree,
		Status:       generation.StatusQueued,
		CurrentStage: generation.StageCostReserve,
		Prompt:       "a small forest at sunrise",
		Model:        "simulated-v1",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusComplete {
		t.Fatalf("final status = %s, want COMPLETE", got.Status)
	}
	if got.ResultAssetID != "ast-svc-1" {
		t.Fatalf("result asset id mismatch: %s", got.ResultAssetID)
	}
}
