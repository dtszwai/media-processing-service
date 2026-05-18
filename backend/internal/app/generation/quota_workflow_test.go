package generation_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// stubPNGOnce renders an 8x8 RGBA PNG. Used by countingProvider so the
// postprocess stamper has a real image to decode + re-encode in tests that
// don't care about pixel content.
var stubPNGBytes = func() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("stubPNG: " + err.Error())
	}
	return buf.Bytes()
}()

// fakeQuotaExhausted always denies.
type fakeQuotaExhausted struct{}

func (fakeQuotaExhausted) Ensure(_ context.Context, _, _ string) error { return nil }
func (fakeQuotaExhausted) Reserve(_ context.Context, _, _ string, _ int64) (bool, int64, error) {
	return false, 0, nil
}
func (fakeQuotaExhausted) Commit(_ context.Context, _, _ string, _ int64) error  { return nil }
func (fakeQuotaExhausted) Release(_ context.Context, _, _ string, _ int64) error { return nil }

// fakeQuotaGranted always grants; records commits.
type fakeQuotaGranted struct {
	commits atomic.Int32
}

func (f *fakeQuotaGranted) Ensure(_ context.Context, _, _ string) error { return nil }
func (f *fakeQuotaGranted) Reserve(_ context.Context, _, _ string, _ int64) (bool, int64, error) {
	return true, 999_999, nil
}
func (f *fakeQuotaGranted) Commit(_ context.Context, _, _ string, _ int64) error {
	f.commits.Add(1)
	return nil
}
func (f *fakeQuotaGranted) Release(_ context.Context, _, _ string, _ int64) error { return nil }

// countingProvider records call count so the test can prove the provider was
// or wasn't invoked. InlineBytes=true; async stubs from genprovider.SyncOnly.
type countingProvider struct {
	genprovider.SyncOnly
	mu    sync.Mutex
	calls atomic.Int32
}

func (p *countingProvider) InlineBytes() bool { return true }
func (p *countingProvider) GenerateSync(_ context.Context, spec generation.JobSpec) (generation.Artifact, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls.Add(1)
	sum := sha256.Sum256(stubPNGBytes)
	return generation.Artifact{
		Bytes:       stubPNGBytes,
		ContentType: "image/png",
		Extension:   "png",
		SHA256:      hex.EncodeToString(sum[:]),
		Metadata: map[string]string{
			"content_safety": "safe",
			"disclosure":     "AI_GENERATED_DISCLOSURE",
		},
	}, nil
}

// Exit criterion: quota exhaustion blocks before provider charge.
func TestWorkflow_BudgetExhausted_DoesNotCallProvider(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	wf.QuotaReserver = fakeQuotaExhausted{}

	ctx := context.Background()
	job := newRunningJob("gen_budget_exhausted")
	_ = repo.CreateJob(ctx, job)

	err := wf.Run(ctx, job.ID)
	if !generation.IsTerminal(err) {
		t.Fatalf("expected terminal BUDGET_EXHAUSTED, got %v", err)
	}
	if generation.AsError(err).Code != "BUDGET_EXHAUSTED" {
		t.Fatalf("expected code BUDGET_EXHAUSTED, got %q", generation.AsError(err).Code)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("provider was called %d times despite quota exhaustion", prov.calls.Load())
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("job status = %s, want FAILED", got.Status)
	}
}

func TestWorkflow_BudgetGranted_CallsProvider(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &countingProvider{}
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-budget-ok"
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), sink)
	quota := &fakeQuotaGranted{}
	wf.QuotaReserver = quota

	ctx := context.Background()
	job := newRunningJob("gen_budget_ok")
	_ = repo.CreateJob(ctx, job)

	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.calls.Load())
	}
	if quota.commits.Load() != 1 {
		t.Fatalf("quota commits = %d, want 1 (charge moment is provider success)", quota.commits.Load())
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusComplete {
		t.Fatalf("status = %s, want COMPLETE", got.Status)
	}
}

func TestProviderSuccessReplay_AttachesQuotaCommitLedger(t *testing.T) {
	repo := &failProviderSuccessOnceRepo{MemRepo: gen.NewMemRepo()}
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	wf.QuotaLedger = fakeQuotaLedger{}

	ctx := context.Background()
	job := newRunningJob("gen_provider_success_replay_commit")
	job.CurrentStage = generation.StageProviderSubmit
	job.StageVersion = 7
	job.PreparedPrompt = "prepared prompt"
	job.PreparedPromptHash = "prepared-hash"
	job.GenerationParamsHash = "params-hash"
	job.BudgetDate = "20260516"
	job.BudgetMicroUSD = gen.DefaultCostMicroUSD(job.OutputType)
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	persisted, _ := repo.GetJob(ctx, "", job.ID)

	if err := wf.AdvanceOneStage(ctx, persisted); err == nil {
		t.Fatalf("first advance should fail after claim completion")
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("provider calls after failed advance = %d, want 1", prov.calls.Load())
	}

	persisted, _ = repo.GetJob(ctx, "", job.ID)
	if err := wf.AdvanceOneStage(ctx, persisted); err != nil {
		t.Fatalf("replay advance: %v", err)
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("provider calls after replay = %d, want 1", prov.calls.Load())
	}
	if repo.replayCommitLedgerOps.Load() != 1 {
		t.Fatalf("replay commit ledger ops = %d, want 1", repo.replayCommitLedgerOps.Load())
	}
}

func TestAdvanceOneStage_LedgerReserveExhaustionPersistsTerminal(t *testing.T) {
	repo := &reserveExhaustionRepo{MemRepo: gen.NewMemRepo()}
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	wf.QuotaReserver = &fakeQuotaGranted{}
	wf.QuotaLedger = fakeQuotaLedger{}

	ctx := context.Background()
	job := newRunningJob("gen_ledger_budget_exhausted")
	job.CurrentStage = generation.StageCostReserve
	job.StageVersion = 1
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	persisted, _ := repo.GetJob(ctx, "", job.ID)

	if err := wf.AdvanceOneStage(ctx, persisted); err != nil {
		t.Fatalf("AdvanceOneStage: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	if got.CurrentStage != generation.StageTerminal {
		t.Fatalf("stage = %s, want TERMINAL", got.CurrentStage)
	}
	if got.Error == nil || got.Error.Code != "BUDGET_EXHAUSTED" {
		t.Fatalf("error = %+v, want BUDGET_EXHAUSTED", got.Error)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want 0", prov.calls.Load())
	}
}

func TestCostReserve_ThreadsSingleQuotaPeriodThroughEnsureAndLedger(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	quota := &recordingPeriodQuota{}
	ledger := &recordingPeriodLedger{}
	wf.QuotaReserver = quota
	wf.QuotaLedger = ledger
	wf.Clock = func() time.Time { return time.Date(2026, 5, 16, 23, 59, 59, 0, time.UTC) }

	ctx := context.Background()
	job := newRunningJob("gen_quota_period")
	job.CurrentStage = generation.StageCostReserve
	job.StageVersion = 1
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	persisted, _ := repo.GetJob(ctx, "", job.ID)

	if err := wf.AdvanceOneStage(ctx, persisted); err != nil {
		t.Fatalf("AdvanceOneStage: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if quota.ensurePeriod != "20260516" {
		t.Fatalf("Ensure period = %q, want 20260516", quota.ensurePeriod)
	}
	if ledger.reservePeriod != "20260516" {
		t.Fatalf("Ledger period = %q, want 20260516", ledger.reservePeriod)
	}
	if got.BudgetDate != "20260516" {
		t.Fatalf("BudgetDate = %q, want 20260516", got.BudgetDate)
	}
}

type reserveExhaustionRepo struct {
	*gen.MemRepo
}

func (r *reserveExhaustionRepo) AdvanceStageAndEnqueue(ctx context.Context, job *generation.Job, result gen.StageResult) error {
	if result.LedgerOp != nil && job.CurrentStage == generation.StageCostReserve && result.NextStage == generation.StagePromptPrepare {
		return generation.Terminal("BUDGET_EXHAUSTED", "tenant daily budget exhausted; provider not called")
	}
	return r.MemRepo.AdvanceStageAndEnqueue(ctx, job, result)
}

type failProviderSuccessOnceRepo struct {
	*gen.MemRepo
	failed                atomic.Bool
	replayCommitLedgerOps atomic.Int32
}

func (r *failProviderSuccessOnceRepo) AdvanceStageAndEnqueue(ctx context.Context, job *generation.Job, result gen.StageResult) error {
	if job.CurrentStage == generation.StageProviderSubmit && result.NextStage == generation.StageOutputModeration {
		if !r.failed.Swap(true) {
			return errors.New("simulated advance failure")
		}
		if result.LedgerOp != nil {
			r.replayCommitLedgerOps.Add(1)
		}
	}
	return r.MemRepo.AdvanceStageAndEnqueue(ctx, job, result)
}

type fakeQuotaLedger struct{}

func (fakeQuotaLedger) LedgerPutReserved(string, string, string, int64, int) gen.LedgerOp {
	return fakeLedgerOp()
}

func (fakeQuotaLedger) LedgerUpdateCommitted(string, string, string, int64) gen.LedgerOp {
	return fakeLedgerOp()
}

func (fakeQuotaLedger) LedgerUpdateReleased(string, string, string, int64) gen.LedgerOp {
	return fakeLedgerOp()
}

type recordingPeriodQuota struct {
	ensurePeriod string
}

func (q *recordingPeriodQuota) Ensure(_ context.Context, _, period string) error {
	q.ensurePeriod = period
	return nil
}

func (q *recordingPeriodQuota) Reserve(_ context.Context, _, period string, _ int64) (bool, int64, error) {
	return true, 0, nil
}

func (q *recordingPeriodQuota) Commit(_ context.Context, _, _ string, _ int64) error {
	return nil
}

func (q *recordingPeriodQuota) Release(_ context.Context, _, _ string, _ int64) error {
	return nil
}

type recordingPeriodLedger struct {
	reservePeriod string
}

func (l *recordingPeriodLedger) LedgerPutReserved(_ string, _ string, period string, _ int64, _ int) gen.LedgerOp {
	l.reservePeriod = period
	return fakeLedgerOp()
}

func (l *recordingPeriodLedger) LedgerUpdateCommitted(string, string, string, int64) gen.LedgerOp {
	return fakeLedgerOp()
}

func (l *recordingPeriodLedger) LedgerUpdateReleased(string, string, string, int64) gen.LedgerOp {
	return fakeLedgerOp()
}

func fakeLedgerOp() gen.LedgerOp {
	return gen.LedgerOp{Items: []kv.WriteOp{
		{Update: &kv.UpdateOp{}},
		{Put: &kv.PutOp{}},
	}}
}
