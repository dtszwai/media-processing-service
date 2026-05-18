package generation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestDLQConsumer_TerminatesCurrentStageWithLastAttemptCode(t *testing.T) {
	ctx := context.Background()
	repo := gen.NewMemRepo()
	now := time.Now().UTC()
	job := generation.Job{
		ID:                   "gen_dlq_retry_exhausted",
		TenantID:             "tenant-test",
		MediaID:              "med-test",
		OutputType:           generation.OutputImage,
		Tier:                 generation.TierFree,
		Status:               generation.StatusRunning,
		CurrentStage:         generation.StageProviderSubmit,
		StageVersion:         1,
		PreparedPrompt:       "prompt",
		PreparedPromptHash:   "prepared-hash",
		GenerationParamsHash: "params-hash",
		BudgetDate:           "20260517",
		BudgetMicroUSD:       1000,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	persisted, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	transient := generation.AsError(generation.Transient("INJECTED_TRANSIENT", "injected transient failure"))
	if err := repo.AdvanceStageAndEnqueue(ctx, persisted, gen.StageResult{
		NextStage:      generation.StageProviderSubmit,
		ResourceClass:  generation.ResourceProvider,
		AttemptsDelta:  1,
		TransientError: transient,
	}); err != nil {
		t.Fatalf("AdvanceStageAndEnqueue transient: %v", err)
	}
	persisted, err = repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob after transient: %v", err)
	}
	body, err := gen.MarshalStageMessage(job.TenantID, job.ID, persisted.CurrentStage, persisted.StageVersion, generation.ResourceProvider, "")
	if err != nil {
		t.Fatalf("MarshalStageMessage: %v", err)
	}

	consumer := gen.DLQConsumer{Repo: repo, Attempts: repo, Ledger: runnerNoopQuotaLedger{}}
	if err := consumer.ProcessMessage(ctx, body); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}

	got, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob terminal: %v", err)
	}
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	if got.CurrentStage != generation.StageTerminal {
		t.Fatalf("stage = %s, want TERMINAL", got.CurrentStage)
	}
	if got.Error == nil || got.Error.Code != "RETRY_EXHAUSTED" {
		t.Fatalf("error = %#v, want RETRY_EXHAUSTED", got.Error)
	}
	if !strings.Contains(got.Error.Message, "INJECTED_TRANSIENT") {
		t.Fatalf("error message = %q, want last attempt code", got.Error.Message)
	}
}

func TestDLQConsumer_DropsTerminalJob(t *testing.T) {
	ctx := context.Background()
	repo := gen.NewMemRepo()
	now := time.Now().UTC()
	job := generation.Job{
		ID:           "gen_dlq_terminal_drop",
		TenantID:     "tenant-test",
		MediaID:      "med-test",
		OutputType:   generation.OutputImage,
		Tier:         generation.TierFree,
		Status:       generation.StatusFailed,
		CurrentStage: generation.StageTerminal,
		StageVersion: 4,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	body, err := gen.MarshalStageMessage(job.TenantID, job.ID, generation.StageProviderSubmit, 3, generation.ResourceProvider, "")
	if err != nil {
		t.Fatalf("MarshalStageMessage: %v", err)
	}

	consumer := gen.DLQConsumer{Repo: repo, Attempts: repo}
	if err := consumer.ProcessMessage(ctx, body); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	got, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.CurrentStage != generation.StageTerminal || got.Status != generation.StatusFailed {
		t.Fatalf("terminal job mutated: stage=%s status=%s", got.CurrentStage, got.Status)
	}
}
