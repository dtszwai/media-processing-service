package generation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// asyncProvider is a controllable async ImageProvider for tests.
type asyncProvider struct {
	submitID  string
	submitErr error
	// pollResponses is consumed in order; last entry is repeated.
	pollResponses []generation.PollStatus
	pollIdx       int
	pollErr       error
	fetchArt      generation.Artifact
	fetchErr      error

	submitCalls   int
	pollCalls     int
	fetchCalls    int
	submittedSpec generation.JobSpec
}

func (p *asyncProvider) InlineBytes() bool { return false }

func (p *asyncProvider) GenerateSync(_ context.Context, _ generation.JobSpec) (generation.Artifact, error) {
	return generation.Artifact{}, errors.New("async provider: GenerateSync not supported")
}

func (p *asyncProvider) SubmitAsync(_ context.Context, spec generation.JobSpec) (string, error) {
	p.submitCalls++
	p.submittedSpec = spec
	return p.submitID, p.submitErr
}

func (p *asyncProvider) PollAsync(_ context.Context, _ string) (generation.PollStatus, error) {
	p.pollCalls++
	if p.pollErr != nil {
		return "", p.pollErr
	}
	if len(p.pollResponses) == 0 {
		return generation.PollPending, nil
	}
	idx := p.pollIdx
	if idx >= len(p.pollResponses) {
		idx = len(p.pollResponses) - 1
	}
	p.pollIdx++
	return p.pollResponses[idx], nil
}

func (p *asyncProvider) FetchAsync(_ context.Context, _ string) (generation.Artifact, error) {
	p.fetchCalls++
	return p.fetchArt, p.fetchErr
}

type countingStager struct {
	gen.StagedArtifactStore
	puts int
}

func (s *countingStager) PutStaged(ctx context.Context, j generation.Job, art generation.Artifact, ttl time.Duration) (gen.StagedRef, error) {
	s.puts++
	return s.StagedArtifactStore.PutStaged(ctx, j, art, ttl)
}

func goodAsyncArtifact() generation.Artifact {
	return generation.Artifact{
		Bytes:       []byte("async-image-bytes"),
		ContentType: "image/png",
		Extension:   "png",
		SHA256:      "aabbccdd",
		Metadata: map[string]string{
			"provider":          "async-test",
			"visible_watermark": "test",
			"content_safety":    "safe",
			"disclosure":        "AI_GENERATED_DISCLOSURE",
		},
	}
}

// TestStageProviderSubmit_AsyncPath_SubmitsAndTransitionsToPoll verifies that when
// InlineBytes=false, stageInference calls SubmitAsync and transitions the job
// to StageProviderWait with the returned ProviderJobID persisted.
func TestStageProviderSubmit_AsyncPath_SubmitsAndTransitionsToPoll(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{submitID: "ext-job-42"}
	sink := gen.NewMemSink()
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_async_submit")
	job.CurrentStage = generation.StageProviderSubmit
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage: %v", err)
	}
	if result.NextStage != generation.StageProviderWait {
		t.Fatalf("NextStage = %s, want PROVIDER_WAIT", result.NextStage)
	}
	if result.ProviderJobID != "ext-job-42" {
		t.Fatalf("ProviderJobID = %q, want ext-job-42", result.ProviderJobID)
	}
	if prov.submitCalls != 1 {
		t.Fatalf("SubmitAsync calls = %d, want 1", prov.submitCalls)
	}
	// ClientRequestID must be a stable vendor idempotency key derived from the
	// prepared prompt hash, not the raw job id.
	if prov.submittedSpec.ClientRequestID == "" || prov.submittedSpec.ClientRequestID == job.ID {
		t.Fatalf("ClientRequestID = %q, want non-empty prepared-prompt key distinct from job id", prov.submittedSpec.ClientRequestID)
	}
}

func TestStageProviderSubmit_AsyncPath_CrashReplayDoesNotResubmit(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{submitID: "ext-job-replay"}
	idem := gen.NewMemIdempotency()
	wf := newTestWorkflow(t, repo, prov, idem, gen.NewMemSink())

	ctx := context.Background()
	job := newRunningJob("gen_async_submit_replay")
	job.CurrentStage = generation.StageProviderSubmit
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage first: %v", err)
	}
	if result.ProviderJobID != "ext-job-replay" {
		t.Fatalf("first ProviderJobID = %q, want ext-job-replay", result.ProviderJobID)
	}

	result, err = wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage replay: %v", err)
	}
	if result.NextStage != generation.StageProviderWait {
		t.Fatalf("replay NextStage = %s, want PROVIDER_WAIT", result.NextStage)
	}
	if result.ProviderJobID != "ext-job-replay" {
		t.Fatalf("replay ProviderJobID = %q, want cached ext-job-replay", result.ProviderJobID)
	}
	if prov.submitCalls != 1 {
		t.Fatalf("SubmitAsync calls after replay = %d, want 1", prov.submitCalls)
	}
}

func TestStageProviderSubmit_AsyncPath_TransientSubmitRetry(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{
		submitID:  "ext-job-after-retry",
		submitErr: errors.New("provider unavailable"),
	}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())

	ctx := context.Background()
	job := newRunningJob("gen_async_submit_retry")
	job.CurrentStage = generation.StageProviderSubmit
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if _, err := wf.RunStage(ctx, &job); err == nil {
		t.Fatalf("first RunStage expected submit error")
	}
	prov.submitErr = nil

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("retry RunStage: %v", err)
	}
	if result.ProviderJobID != "ext-job-after-retry" {
		t.Fatalf("ProviderJobID = %q, want ext-job-after-retry", result.ProviderJobID)
	}
	if prov.submitCalls != 2 {
		t.Fatalf("SubmitAsync calls = %d, want 2", prov.submitCalls)
	}
}

// TestStageProviderWait_Pending_ReEnqueues verifies that PollPending causes
// the stage to stay at StageProviderWait.
func TestStageProviderWait_Pending_ReEnqueues(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{
		submitID:      "ext-job-pending",
		pollResponses: []generation.PollStatus{generation.PollPending},
	}
	sink := gen.NewMemSink()
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_poll_pending")
	job.CurrentStage = generation.StageProviderWait
	job.ProviderJobID = "ext-job-pending"
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage: %v", err)
	}
	if result.NextStage != generation.StageProviderWait {
		t.Fatalf("NextStage = %s, want PROVIDER_WAIT", result.NextStage)
	}
	if len(sink.Stored) != 0 {
		t.Fatalf("sink should not be called on Pending; got %d", len(sink.Stored))
	}
}

// TestStageProviderWait_Ready_StagesAndAdvances verifies that PollReady
// fetches the artifact, writes it to the staging area (NOT the final sink),
// and transitions to StageDisclosurePostprocess. ResultAssetID is set only by DISCLOSURE_POSTPROCESS.
func TestStageProviderWait_Ready_StagesAndAdvances(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{
		submitID:      "ext-job-ready",
		pollResponses: []generation.PollStatus{generation.PollReady},
		fetchArt:      goodAsyncArtifact(),
	}
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-async-1"
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_poll_ready")
	job.CurrentStage = generation.StageProviderWait
	job.ProviderJobID = "ext-job-ready"
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage: %v", err)
	}
	if result.NextStage != generation.StageOutputModeration {
		t.Fatalf("NextStage = %s, want OUTPUT_MODERATION", result.NextStage)
	}
	// Final asset id is set by DISCLOSURE_POSTPROCESS, not INFER. INFER only stages.
	if result.ResultAssetID != "" {
		t.Fatalf("ResultAssetID = %q, want empty at INFER end (DISCLOSURE_POSTPROCESS sets it)", result.ResultAssetID)
	}
	// The final sink must not be touched until DISCLOSURE_POSTPROCESS runs.
	if len(sink.Stored) != 0 {
		t.Fatalf("sink stored %d artifacts at INFER end, want 0 (DISCLOSURE_POSTPROCESS sinks)", len(sink.Stored))
	}
}

func TestStageProviderWait_Ready_CrashReplayDoesNotFetchOrRestage(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{
		submitID:      "ext-job-ready-replay",
		pollResponses: []generation.PollStatus{generation.PollReady, generation.PollFailed},
		fetchArt:      goodAsyncArtifact(),
	}
	stager := &countingStager{StagedArtifactStore: gen.NewMemStaging()}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	wf.Stager = stager

	ctx := context.Background()
	job := newRunningJob("gen_poll_ready_replay")
	job.CurrentStage = generation.StageProviderWait
	job.ProviderJobID = "ext-job-ready-replay"
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage first: %v", err)
	}
	if result.NextStage != generation.StageOutputModeration {
		t.Fatalf("first NextStage = %s, want OUTPUT_MODERATION", result.NextStage)
	}

	result, err = wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage replay: %v", err)
	}
	if result.NextStage != generation.StageOutputModeration {
		t.Fatalf("replay NextStage = %s, want OUTPUT_MODERATION", result.NextStage)
	}
	if prov.fetchCalls != 1 {
		t.Fatalf("FetchAsync calls after replay = %d, want 1", prov.fetchCalls)
	}
	if prov.pollCalls != 1 {
		t.Fatalf("PollAsync calls after replay = %d, want 1", prov.pollCalls)
	}
	if stager.puts != 1 {
		t.Fatalf("PutStaged calls after replay = %d, want 1", stager.puts)
	}
}

func TestStageProviderWait_Ready_TransientFetchRetry(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{
		submitID:      "ext-job-fetch-retry",
		pollResponses: []generation.PollStatus{generation.PollReady},
		fetchArt:      goodAsyncArtifact(),
		fetchErr:      errors.New("fetch timeout"),
	}
	stager := &countingStager{StagedArtifactStore: gen.NewMemStaging()}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	wf.Stager = stager

	ctx := context.Background()
	job := newRunningJob("gen_poll_fetch_retry")
	job.CurrentStage = generation.StageProviderWait
	job.ProviderJobID = "ext-job-fetch-retry"
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if _, err := wf.RunStage(ctx, &job); err == nil {
		t.Fatalf("first RunStage expected fetch error")
	}
	prov.fetchErr = nil

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("retry RunStage: %v", err)
	}
	if result.NextStage != generation.StageOutputModeration {
		t.Fatalf("NextStage = %s, want OUTPUT_MODERATION", result.NextStage)
	}
	if prov.fetchCalls != 2 {
		t.Fatalf("FetchAsync calls = %d, want 2", prov.fetchCalls)
	}
	if stager.puts != 1 {
		t.Fatalf("PutStaged calls = %d, want 1", stager.puts)
	}
}

// TestStageProviderWait_Failed_TerminatesJob verifies that PollFailed
// produces a terminal result.
func TestStageProviderWait_Failed_TerminatesJob(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{
		submitID:      "ext-job-fail",
		pollResponses: []generation.PollStatus{generation.PollFailed},
	}
	sink := gen.NewMemSink()
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)

	ctx := context.Background()
	job := newRunningJob("gen_poll_failed")
	job.CurrentStage = generation.StageProviderWait
	job.ProviderJobID = "ext-job-fail"
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage should not return an error for PollFailed; got %v", err)
	}
	if result.NextStage != gen.StageTerminal {
		t.Fatalf("NextStage = %s, want TERMINAL", result.NextStage)
	}
	if result.TerminalError == nil || result.TerminalError.Code != "PROVIDER_JOB_FAILED" {
		t.Fatalf("TerminalError = %v, want code PROVIDER_JOB_FAILED", result.TerminalError)
	}
	if len(sink.Stored) != 0 {
		t.Fatalf("sink should not be called on PollFailed; got %d", len(sink.Stored))
	}
}

// TestStageProviderWait_MissingProviderJobID_IsTerminal verifies that
// polling with an empty ProviderJobID fails terminally (defensive guard).
func TestStageProviderWait_MissingProviderJobID_IsTerminal(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &asyncProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())

	ctx := context.Background()
	job := newRunningJob("gen_poll_no_id")
	job.CurrentStage = generation.StageProviderWait
	job.ProviderJobID = "" // missing
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	_, err := wf.RunStage(ctx, &job)
	if err == nil {
		t.Fatalf("expected terminal error, got nil")
	}
	if !generation.IsTerminal(err) {
		t.Fatalf("error should be terminal: %v", err)
	}
}
