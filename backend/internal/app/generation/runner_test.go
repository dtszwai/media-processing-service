package generation_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

func TestStageRunner_ProviderUnavailableTerminatesJob(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	registry, err := bootstrap.NewProviderRegistry(app.GenerationConfig{})
	if err != nil {
		t.Fatalf("NewProviderRegistry: %v", err)
	}

	repo := gen.NewMemRepo()
	now := time.Now().UTC()
	job := generation.Job{
		ID:           "gen_provider_unavailable",
		TenantID:     "tenant-test",
		MediaID:      "med-test",
		OutputType:   generation.OutputImage,
		Provider:     "codex",
		Tier:         generation.TierFree,
		Status:       generation.StatusRunning,
		CurrentStage: generation.StageProviderSubmit,
		StageVersion: 1,
		Prompt:       "same prompt",
		Model:        "gpt-5.5",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	body, err := gen.MarshalStageMessage(job.TenantID, job.ID, generation.StageProviderSubmit, 1, generation.ResourceProvider, "")
	if err != nil {
		t.Fatalf("MarshalStageMessage: %v", err)
	}

	runner := gen.StageRunner{Repo: repo, Pickers: registry}
	if err := runner.ProcessMessage(context.Background(), body); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	got, err := repo.GetJob(context.Background(), job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	if got.CurrentStage != generation.StageTerminal {
		t.Fatalf("stage = %s, want TERMINAL", got.CurrentStage)
	}
	if got.Error == nil || got.Error.Code != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("terminal error = %#v, want PROVIDER_UNAVAILABLE", got.Error)
	}
}

func TestStageRunner_TransientFailureEnqueuesFreshRetryMessage(t *testing.T) {
	ctx := context.Background()
	repo := gen.NewMemRepo()
	var outboxBodies [][]byte
	repo.OutboxObserver = func(_ generation.Stage, body []byte) {
		outboxBodies = append(outboxBodies, append([]byte(nil), body...))
	}
	prov := simulated.New()
	runner := newProductionShapeStageRunner(t, repo, prov)

	job := newProviderSubmitJob("gen_runner_transient_retry")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	prov.InjectFailures(job.ID, simulated.FailurePlan{TransientFailures: 1})
	originalBody := mustStageMessage(t, job, job.StageVersion)

	if err := runner.ProcessMessage(ctx, originalBody); err != nil {
		t.Fatalf("transient ProcessMessage should ack the originating message after durably enqueuing the retry, got err = %v", err)
	}
	got, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob after first attempt: %v", err)
	}
	if got.StageVersion != 2 {
		t.Fatalf("stage version after transient = %d, want 2", got.StageVersion)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts after transient = %d, want 1", got.Attempts)
	}
	if got.CurrentStage != generation.StageProviderSubmit {
		t.Fatalf("stage after transient = %s, want PROVIDER_SUBMIT", got.CurrentStage)
	}
	if len(outboxBodies) != 1 {
		t.Fatalf("outbox rows after transient = %d, want 1", len(outboxBodies))
	}
	retryMsg := unmarshalStageMessage(t, outboxBodies[0])
	if retryMsg.Stage != generation.StageProviderSubmit {
		t.Fatalf("retry stage = %s, want PROVIDER_SUBMIT", retryMsg.Stage)
	}
	if retryMsg.StageVersion != 2 {
		t.Fatalf("retry version = %d, want 2", retryMsg.StageVersion)
	}

	if err := runner.ProcessMessage(ctx, originalBody); err != nil {
		t.Fatalf("stale original redelivery should be acknowledged: %v", err)
	}
	got, err = repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob after stale redelivery: %v", err)
	}
	if got.StageVersion != 2 || got.Attempts != 1 || got.CurrentStage != generation.StageProviderSubmit {
		t.Fatalf("stale redelivery mutated job: stage=%s version=%d attempts=%d", got.CurrentStage, got.StageVersion, got.Attempts)
	}

	if err := runner.ProcessMessage(ctx, outboxBodies[0]); err != nil {
		t.Fatalf("retry ProcessMessage: %v", err)
	}
	got, err = repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob after retry: %v", err)
	}
	if got.CurrentStage != generation.StageOutputModeration {
		t.Fatalf("stage after retry = %s, want OUTPUT_MODERATION", got.CurrentStage)
	}
	if got.StageVersion != 3 {
		t.Fatalf("stage version after retry = %d, want 3", got.StageVersion)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts after retry = %d, want 1", got.Attempts)
	}
}

func TestStageRunner_TransientFailureExhaustsRetries(t *testing.T) {
	ctx := context.Background()
	repo := gen.NewMemRepo()
	var outboxBodies [][]byte
	repo.OutboxObserver = func(_ generation.Stage, body []byte) {
		outboxBodies = append(outboxBodies, append([]byte(nil), body...))
	}
	prov := simulated.New()
	runner := newProductionShapeStageRunner(t, repo, prov)

	job := newProviderSubmitJob("gen_runner_retry_exhausted")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	prov.InjectFailures(job.ID, simulated.FailurePlan{TransientFailures: 3})

	body := mustStageMessage(t, job, job.StageVersion)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := runner.ProcessMessage(ctx, body); err != nil {
			t.Fatalf("attempt %d transient should ack: %v", attempt, err)
		}
		if len(outboxBodies) != attempt {
			t.Fatalf("outbox rows after attempt %d = %d, want %d", attempt, len(outboxBodies), attempt)
		}
		body = outboxBodies[len(outboxBodies)-1]
	}

	if err := runner.ProcessMessage(ctx, body); err != nil {
		t.Fatalf("exhausting attempt should persist terminal failure and ack: %v", err)
	}
	got, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob after exhaustion: %v", err)
	}
	if got.Status != generation.StatusFailed {
		t.Fatalf("status after exhaustion = %s, want FAILED", got.Status)
	}
	if got.CurrentStage != generation.StageTerminal {
		t.Fatalf("stage after exhaustion = %s, want TERMINAL", got.CurrentStage)
	}
	if got.Error == nil || got.Error.Code != "RETRY_EXHAUSTED" {
		t.Fatalf("terminal error after exhaustion = %#v, want RETRY_EXHAUSTED", got.Error)
	}
	if got.Attempts != 3 {
		t.Fatalf("attempts after exhaustion = %d, want 3", got.Attempts)
	}
	if len(outboxBodies) != 2 {
		t.Fatalf("outbox rows after exhaustion = %d, want 2", len(outboxBodies))
	}
}

func newProviderSubmitJob(id string) generation.Job {
	now := time.Now().UTC()
	return generation.Job{
		ID:                   id,
		TenantID:             "tenant-test",
		MediaID:              "med-test",
		OutputType:           generation.OutputImage,
		Provider:             "simulated",
		Tier:                 generation.TierFree,
		Status:               generation.StatusRunning,
		CurrentStage:         generation.StageProviderSubmit,
		StageVersion:         1,
		Prompt:               "same prompt",
		PreparedPrompt:       "same prompt",
		PreparedPromptHash:   "prepared-test-hash",
		PromptSpecVersion:    "prompt-policy-v1",
		GenerationParamsHash: "params-test-hash",
		Model:                "simulated-v1",
		BudgetDate:           "20260517",
		BudgetMicroUSD:       1000,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func mustStageMessage(t *testing.T, job generation.Job, version uint64) []byte {
	t.Helper()
	body, err := gen.MarshalStageMessage(job.TenantID, job.ID, job.CurrentStage, version, generation.ResourceProvider, "")
	if err != nil {
		t.Fatalf("MarshalStageMessage: %v", err)
	}
	return body
}

func unmarshalStageMessage(t *testing.T, body []byte) gen.StageMessage {
	t.Helper()
	var msg gen.StageMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatalf("stage message json: %v", err)
	}
	return msg
}

func newProductionShapeStageRunner(t *testing.T, repo *gen.MemRepo, provider genprovider.Provider) gen.StageRunner {
	t.Helper()
	return gen.StageRunner{
		Repo:           repo,
		Idem:           gen.NewMemIdempotency(),
		Sink:           gen.NewMemSink(),
		Stager:         gen.NewMemStaging(),
		ImageStamper:   testStamper(t),
		LeaseRunner:    gen.NewLeaseScopedRunner(nil),
		Quota:          runnerNoopQuota{},
		Ledger:         runnerNoopQuotaLedger{},
		Sealer:         runnerNoopPromptSealer{},
		Pickers:        runnerStaticProviderResolver{provider: provider},
		Moderator:      safetyapp.NewSimulatedModerator(),
		PromptEnhancer: &gen.PassthroughEnhancer{},
		AuditRecorder:  auditapp.NoopRecorder{},
	}
}

type runnerStaticProviderResolver struct {
	provider genprovider.Provider
}

func (r runnerStaticProviderResolver) PickForJob(generation.OutputType, string) (genprovider.Provider, error) {
	return r.provider, nil
}

type runnerNoopQuota struct{}

func (runnerNoopQuota) Ensure(context.Context, string, string) error { return nil }

func (runnerNoopQuota) Reserve(context.Context, string, string, int64) (bool, int64, error) {
	return true, 0, nil
}

func (runnerNoopQuota) Commit(context.Context, string, string, int64) error { return nil }

func (runnerNoopQuota) Release(context.Context, string, string, int64) error { return nil }

type runnerNoopQuotaLedger struct{}

func (runnerNoopQuotaLedger) LedgerPutReserved(string, string, string, int64, int) gen.LedgerOp {
	return gen.LedgerOp{}
}

func (runnerNoopQuotaLedger) LedgerUpdateCommitted(string, string, string, int64) gen.LedgerOp {
	return gen.LedgerOp{}
}

func (runnerNoopQuotaLedger) LedgerUpdateReleased(string, string, string, int64) gen.LedgerOp {
	return gen.LedgerOp{}
}

type runnerNoopPromptSealer struct{}

func (runnerNoopPromptSealer) Seal(_ context.Context, _, _ string, plaintext string) ([]byte, error) {
	return []byte(plaintext), nil
}

func (runnerNoopPromptSealer) Unseal(_ context.Context, _, _ string, ciphertext []byte) (string, error) {
	return string(ciphertext), nil
}
