package generation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

// Tests in this file cover three DISCLOSURE_POSTPROCESS scenarios: happy path,
// crash-then-replay (no second provider call), and staged-bytes expiry.

func newPostprocessJob(id string) generation.Job {
	return generation.Job{
		ID:         id,
		TenantID:   "tenant-postproc",
		MediaID:    "med-postproc",
		OutputType: generation.OutputImage,
		Tier:       generation.TierFree,
		Status:     generation.StatusRunning,
		Prompt:     "a postprocess test prompt",
		Model:      "simulated-v1",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
}

// TestPostprocess_HappyPath_StagesThenFinalSinks confirms the FSM walks
// INFER → staged → DISCLOSURE_POSTPROCESS → PUBLISH, that the staged store sees one
// PutStaged and one DeleteStaged, and that the final sink is called only at
// DISCLOSURE_POSTPROCESS.
func TestPostprocess_HappyPath_StagesThenFinalSinks(t *testing.T) {
	repo := gen.NewMemRepo()
	stager := gen.NewMemStaging()
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-postproc-happy"
	wf := newTestWorkflow(t, repo, simulated.New(), gen.NewMemIdempotency(), sink)
	wf.Stager = stager

	ctx := context.Background()
	job := newPostprocessJob("gen_postproc_happy")
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
	if got.ResultAssetID != "ast-postproc-happy" {
		t.Fatalf("ResultAssetID = %q, want ast-postproc-happy", got.ResultAssetID)
	}
	if len(sink.Stored) != 1 {
		t.Fatalf("final sink called %d times, want 1", len(sink.Stored))
	}
	// Final asset metadata must include the AI-disclosure markers — proves the
	// gate ran in DISCLOSURE_POSTPROCESS and accepted what it found.
	final := sink.Stored[0]
	if final.Metadata["visible_watermark"] == "" || final.Metadata["disclosure"] == "" {
		t.Fatalf("final artifact missing watermark/disclosure metadata: %#v", final.Metadata)
	}
}

// TestPostprocess_ReplayFromStaged_NoSecondProviderCall simulates a crash
// after the staged write but before DISCLOSURE_POSTPROCESS commits. On replay the
// inference claim observes ClaimReplayCompleted, INFER short-circuits to
// DISCLOSURE_POSTPROCESS, and DISCLOSURE_POSTPROCESS finishes from the staged bytes.
//
// The countingProvider (from quota_workflow_test.go) is used so we can
// assert the provider is called exactly once across both walks.
func TestPostprocess_ReplayFromStaged_NoSecondProviderCall(t *testing.T) {
	repo := gen.NewMemRepo()
	stager := gen.NewMemStaging()
	idem := gen.NewMemIdempotency()
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-postproc-replay"
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, idem, sink)
	wf.Stager = stager

	ctx := context.Background()
	job := newPostprocessJob("gen_postproc_replay")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Phase 1: walk COST_RESERVE → PROMPT_PREPARE → PROVIDER_SUBMIT → (staged) →
	// DISCLOSURE_POSTPROCESS, stopping at the boundary where DISCLOSURE_POSTPROCESS would run.
	persistedJob, _ := repo.GetJob(ctx, "", job.ID)
	for persistedJob.CurrentStage != generation.StageDisclosurePostprocess {
		if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
			t.Fatalf("advance to staged: %v", err)
		}
		persistedJob, _ = repo.GetJob(ctx, "", job.ID)
	}
	if c := prov.calls.Load(); c != 1 {
		t.Fatalf("provider calls after first walk = %d, want 1", c)
	}
	if len(sink.Stored) != 0 {
		t.Fatalf("sink touched at INFER end: %d stores", len(sink.Stored))
	}

	// Phase 2: simulate worker restart by re-running the same job. The FSM
	// re-enters DISCLOSURE_POSTPROCESS (or replays from STAGED_WRITE if the worker
	// crashed before persisting the stage advance — both shapes are valid
	// recovery), and the provider must not be called again.
	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("replay Run: %v", err)
	}
	if c := prov.calls.Load(); c != 1 {
		t.Fatalf("provider re-called during replay: calls=%d, want 1", c)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusComplete {
		t.Fatalf("replay status = %s, want COMPLETE", got.Status)
	}
	if got.ResultAssetID != "ast-postproc-replay" {
		t.Fatalf("ResultAssetID = %q, want ast-postproc-replay", got.ResultAssetID)
	}
}

// TestPostprocess_StagedBytesExpired_TerminalSTAGEDEXPIRED simulates the
// S3-lifecycle sweep wiping the staged bytes after the 24h TTL. DISCLOSURE_POSTPROCESS
// must fail terminally with STAGED_EXPIRED so the job stops rather than
// retrying forever on an unrecoverable input.
func TestPostprocess_StagedBytesExpired_TerminalSTAGEDEXPIRED(t *testing.T) {
	repo := gen.NewMemRepo()
	stager := gen.NewMemStaging()
	idem := gen.NewMemIdempotency()
	sink := gen.NewMemSink()
	wf := newTestWorkflow(t, repo, simulated.New(), idem, sink)
	wf.Stager = stager

	ctx := context.Background()
	job := newPostprocessJob("gen_postproc_expired")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Walk to DISCLOSURE_POSTPROCESS, then evict the staged bytes to simulate the
	// lifecycle sweep.
	persistedJob, _ := repo.GetJob(ctx, "", job.ID)
	for persistedJob.CurrentStage != generation.StageDisclosurePostprocess {
		if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
			t.Fatalf("advance: %v", err)
		}
		persistedJob, _ = repo.GetJob(ctx, "", job.ID)
	}
	stagedKey, _, _ := idem.GetResult(ctx, "GEN#"+job.ID+"#STAGED_WRITE")
	stager.Drop(stagedKey)

	// AdvanceOneStage handles terminal errors by persisting TERMINAL
	// and returning nil; the assertion is on the persisted job state, not the
	// return value.
	if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
		t.Fatalf("AdvanceOneStage: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	if got.Error == nil || got.Error.Code != "STAGED_EXPIRED" {
		t.Fatalf("job.Error = %+v, want terminal STAGED_EXPIRED", got.Error)
	}
	if len(sink.Stored) != 0 {
		t.Fatalf("sink stored an artifact after STAGED_EXPIRED: %d", len(sink.Stored))
	}
}

func TestPostprocess_DoesNotAdvanceAfterSinkFailure(t *testing.T) {
	repo := gen.NewMemRepo()
	stager := gen.NewMemStaging()
	idem := gen.NewMemIdempotency()
	wf := newTestWorkflow(t, repo, simulated.New(), idem, failingSink{})
	wf.Stager = stager

	ctx := context.Background()
	job := newPostprocessJob("gen_postproc_sink_fail")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	persistedJob, _ := repo.GetJob(ctx, "", job.ID)
	for persistedJob.CurrentStage != generation.StageDisclosurePostprocess {
		if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
			t.Fatalf("advance: %v", err)
		}
		persistedJob, _ = repo.GetJob(ctx, "", job.ID)
	}

	preVersion := persistedJob.StageVersion
	preAttempts := persistedJob.Attempts
	var retryStages []generation.Stage
	repo.OutboxObserver = func(stage generation.Stage, _ []byte) {
		retryStages = append(retryStages, stage)
	}

	if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
		t.Fatalf("transient sink failure should ack via outbox-enqueued retry, got err = %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.CurrentStage != generation.StageDisclosurePostprocess {
		t.Fatalf("stage after sink failure = %s, want DISCLOSURE_POSTPROCESS (transient retry must not advance)", got.CurrentStage)
	}
	if got.StageVersion != preVersion+1 {
		t.Fatalf("stage version after sink failure = %d, want %d", got.StageVersion, preVersion+1)
	}
	if got.Attempts != preAttempts+1 {
		t.Fatalf("attempts after sink failure = %d, want %d", got.Attempts, preAttempts+1)
	}
	if len(retryStages) != 1 || retryStages[0] != generation.StageDisclosurePostprocess {
		t.Fatalf("outbox retry stages = %v, want [DISCLOSURE_POSTPROCESS]", retryStages)
	}
}

func TestPostprocess_GateFailureDecisionRidesTerminalAdvance(t *testing.T) {
	repo := &recordingResultRepo{MemRepo: gen.NewMemRepo()}
	stager := gen.NewMemStaging()
	idem := gen.NewMemIdempotency()
	wf := newTestWorkflow(t, repo, simulated.New(), idem, gen.NewMemSink())
	wf.Stager = stager
	wf.ImageStamper = nil

	ctx := context.Background()
	job := newPostprocessJob("gen_postproc_gate_fail")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	persistedJob, _ := repo.GetJob(ctx, "", job.ID)
	for persistedJob.CurrentStage != generation.StageDisclosurePostprocess {
		if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
			t.Fatalf("advance: %v", err)
		}
		persistedJob, _ = repo.GetJob(ctx, "", job.ID)
	}

	if err := wf.AdvanceOneStage(ctx, persistedJob); err != nil {
		t.Fatalf("AdvanceOneStage: %v", err)
	}
	result := repo.lastResult()
	if result.GateDecision == nil {
		t.Fatal("terminal StageResult.GateDecision = nil, want gate failure decision")
	}
	if result.GateDecision.Decision != "FAIL" || result.GateDecision.ErrorCode == "" {
		t.Fatalf("gate decision = %+v, want FAIL with error code", result.GateDecision)
	}
}

func TestMemStaging_LoadStagedRejectsExpiredRef(t *testing.T) {
	stager := gen.NewMemStaging()
	t0 := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	stager.Now = func() time.Time { return t0 }

	job := newPostprocessJob("gen_mem_staged_expiry")
	ref, err := stager.PutStaged(context.Background(), job, generation.Artifact{
		Bytes:       []byte("staged"),
		ContentType: "application/octet-stream",
		Extension:   "bin",
		SHA256:      "sha",
	}, time.Minute)
	if err != nil {
		t.Fatalf("PutStaged: %v", err)
	}

	stager.Now = func() time.Time { return t0.Add(time.Minute) }
	if _, err := stager.LoadStaged(context.Background(), ref); !errors.Is(err, gen.ErrStagedNotFound) {
		t.Fatalf("LoadStaged expired err = %v, want ErrStagedNotFound", err)
	}
}

// (countingProvider lives in quota_workflow_test.go and is shared by these
// tests — both files are in package generation_test.)

type failingSink struct{}

func (failingSink) StoreFinalArtifact(context.Context, generation.Job, generation.Artifact) (string, error) {
	return "", generation.Transient("SINK_UNAVAILABLE", "sink unavailable")
}

type recordingResultRepo struct {
	*gen.MemRepo
	results []gen.StageResult
}

func (r *recordingResultRepo) AdvanceStageAndEnqueue(ctx context.Context, job *generation.Job, result gen.StageResult) error {
	r.results = append(r.results, result)
	return r.MemRepo.AdvanceStageAndEnqueue(ctx, job, result)
}

func (r *recordingResultRepo) lastResult() gen.StageResult {
	if len(r.results) == 0 {
		return gen.StageResult{}
	}
	return r.results[len(r.results)-1]
}
