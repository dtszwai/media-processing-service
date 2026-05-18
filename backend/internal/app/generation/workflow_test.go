package generation_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

func newRunningJob(id string) generation.Job {
	return generation.Job{
		ID:                   id,
		TenantID:             "tenant-test",
		MediaID:              "med-test",
		OutputType:           generation.OutputImage,
		Tier:                 generation.TierFree,
		Status:               generation.StatusRunning,
		Prompt:               "a small test prompt",
		PreparedPrompt:       "a small test prompt",
		PreparedPromptHash:   "prepared-test-hash",
		PromptSpecVersion:    "prompt-policy-v1",
		GenerationParamsHash: "params-test-hash",
		Model:                "simulated-v1",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
}

// Exit criterion 1: submit simulated image → COMPLETE.
func TestWorkflow_Completes_OnHappyPath(t *testing.T) {
	repo := gen.NewMemRepo()
	var stages []generation.Stage
	repo.OutboxObserver = func(stage generation.Stage, body []byte) {
		stages = append(stages, stage)
		if len(body) == 0 {
			t.Fatalf("outbox body missing for stage %s", stage)
		}
		var msg gen.StageMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Fatalf("outbox body invalid json: %v", err)
		}
		if msg.Stage != stage {
			t.Fatalf("outbox body stage = %s, observer stage = %s", msg.Stage, stage)
		}
	}
	prov := simulated.New()
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-happy"
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_happy")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusComplete {
		t.Fatalf("status = %s, want COMPLETE", got.Status)
	}
	if got.ResultAssetID != "ast-happy" {
		t.Fatalf("result asset = %q, want ast-happy", got.ResultAssetID)
	}
	if len(sink.Stored) != 1 {
		t.Fatalf("sink stored %d artifacts, want 1", len(sink.Stored))
	}
	if sink.Stored[0].ContentType != "image/png" {
		t.Fatalf("artifact content_type = %q", sink.Stored[0].ContentType)
	}
	wantStages := []generation.Stage{
		generation.StageCostReserve,
		generation.StagePromptPrepare,
		generation.StageProviderSubmit,
		generation.StageOutputModeration,
		generation.StageDisclosurePostprocess,
		generation.StagePublish,
	}
	if len(stages) != len(wantStages) {
		t.Fatalf("outbox stages = %v, want %v", stages, wantStages)
	}
	for i := range wantStages {
		if stages[i] != wantStages[i] {
			t.Fatalf("outbox stage[%d] = %s, want %s", i, stages[i], wantStages[i])
		}
	}
}

// Exit criterion 2: forced retry retries once. The provider returns a
// transient error on the first call; on the second call (re-running the
// workflow) it succeeds.
func TestWorkflow_RetriesTransient_ThenCompletes(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := simulated.New()
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-retry"
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_retry")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	prov.InjectFailures(job.ID, simulated.FailurePlan{TransientFailures: 1})

	// First run: transient error returned, workflow surfaces it for retry.
	err := wf.Run(ctx, job.ID)
	if err == nil {
		t.Fatalf("expected transient error on first run")
	}
	if generation.IsTerminal(err) {
		t.Fatalf("first run should NOT be terminal: %v", err)
	}
	attempts1, _ := repo.GetJob(ctx, "", job.ID)
	if attempts1.Attempts != 1 {
		t.Fatalf("attempts after first run = %d, want 1", attempts1.Attempts)
	}

	// Second run: provider succeeds, workflow completes.
	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("second run: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusComplete {
		t.Fatalf("after retry status = %s, want COMPLETE", got.Status)
	}
}

// Exit criterion 3: terminal code does not retry.
func TestWorkflow_TerminalCode_DoesNotRetry(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := simulated.New()
	sink := gen.NewMemSink()
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_terminal")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	prov.InjectFailures(job.ID, simulated.FailurePlan{TerminalCode: "PROVIDER_AUTH_FAILED"})

	err := wf.Run(ctx, job.ID)
	if err == nil {
		t.Fatalf("expected terminal error")
	}
	if !generation.IsTerminal(err) {
		t.Fatalf("error not classified terminal: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	if got.Attempts != 0 {
		t.Fatalf("attempts incremented on terminal error: %d", got.Attempts)
	}
	if len(sink.Stored) != 0 {
		t.Fatalf("sink stored artifact on terminal failure: %d", len(sink.Stored))
	}
}

func TestWorkflow_EmptyPrompt_TerminalUpfront(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := simulated.New()
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())

	ctx := context.Background()
	job := newRunningJob("gen_empty_prompt")
	job.Prompt = ""
	_ = repo.CreateJob(ctx, job)

	err := wf.Run(ctx, job.ID)
	if !generation.IsTerminal(err) {
		t.Fatalf("expected terminal EMPTY_PROMPT, got %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
}
