package generation_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

func TestSimulatedLifecycle_SubmitQueueRunnerStoresGatedAssetAndChargesOnce(t *testing.T) {
	cases := []struct {
		name              string
		outputType        generation.OutputType
		tier              generation.Tier
		prompt            string
		wantContentPrefix string
		wantExtension     string
		wantCostMicroUSD  int64
		wantQueues        []string
	}{
		{
			name:              "image",
			outputType:        generation.OutputImage,
			tier:              generation.TierPaid,
			prompt:            "a deterministic queue-driven image lifecycle test",
			wantContentPrefix: "image/",
			wantExtension:     "png",
			wantCostMicroUSD:  gen.DefaultCostMicroUSD(generation.OutputImage),
			wantQueues: []string{
				"generation-jobs-paid-fast",
				"generation-jobs-paid-fast",
				"generation-jobs-paid-fast",
				"generation-jobs-paid-provider",
				"generation-jobs-paid-fast",
				"generation-jobs-paid-image-process",
				"generation-jobs-paid-fast",
			},
		},
		{
			name:              "audio",
			outputType:        generation.OutputAudio,
			tier:              generation.TierFree,
			prompt:            "a deterministic queue-driven audio lifecycle test",
			wantContentPrefix: "audio/",
			wantExtension:     "wav",
			wantCostMicroUSD:  gen.DefaultCostMicroUSD(generation.OutputAudio),
			wantQueues: []string{
				"generation-jobs-free-fast",
				"generation-jobs-free-fast",
				"generation-jobs-free-fast",
				"generation-jobs-free-provider",
				"generation-jobs-free-fast",
				"generation-jobs-free-fast",
				"generation-jobs-free-fast",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := gen.NewMemRepo()
			queue := &lifecycleStageQueue{}
			repo.OutboxObserver = func(_ generation.Stage, body []byte) {
				queue.push(body)
			}

			provider := &countingLifecycleProvider{inner: simulated.New()}
			sink := &recordingLifecycleSink{}
			quota := &recordingLifecycleQuota{}
			ledger := &recordingLifecycleLedger{}
			usage := &recordingLifecycleUsage{}
			submitter := &lifecycleSubmitter{repo: repo, enqueue: queue.push}
			now := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
			nextID := deterministicIDs("media", "job", "asset")
			service := gen.SubmissionService{
				Submitter: submitter,
				Now:       func() time.Time { return now },
				NewID:     nextID,
			}

			result, err := service.Create(ctx, gen.SubmitCommand{
				TenantID:        "tenant-lifecycle",
				UserID:          "user-lifecycle",
				Prompt:          tc.prompt,
				Provider:        "simulated",
				Model:           "simulated-v1",
				OutputType:      tc.outputType,
				Tier:            tc.tier,
				ResolutionLabel: "64x64",
				Seed:            42,
				IdempotencyKey:  "idem-" + tc.name,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if result.Replay {
				t.Fatal("first submit unexpectedly replayed")
			}
			if result.Job.ID != "gen_job" || result.Job.MediaID != "med_media" || result.Job.ResultAssetID != "ast_asset" {
				t.Fatalf("allocated ids = job:%s media:%s asset:%s", result.Job.ID, result.Job.MediaID, result.Job.ResultAssetID)
			}
			if submitter.input.Media.Lifecycle != media.LifecycleRunning {
				t.Fatalf("submitted media lifecycle = %s, want RUNNING", submitter.input.Media.Lifecycle)
			}
			if submitter.input.ResultAsset.Lifecycle != media.AssetLifecyclePending {
				t.Fatalf("submitted result asset lifecycle = %s, want PENDING", submitter.input.ResultAsset.Lifecycle)
			}

			runner := gen.StageRunner{
				Repo:          repo,
				Idem:          gen.NewMemIdempotency(),
				Sink:          sink,
				Stager:        gen.NewMemStaging(),
				LeaseRunner:   gen.NewLeaseScopedRunner(nil),
				Quota:         quota,
				Ledger:        ledger,
				Sealer:        lifecyclePromptSealer{},
				Pickers:       lifecycleProviderResolver{provider: provider},
				Moderator:     safetyapp.NewSimulatedModerator(),
				AuditRecorder: auditapp.NoopRecorder{},
				UsageMeter:    usage,
			}

			processed, providerSubmitBody := drainLifecycleQueue(t, ctx, &runner, queue, result.Job)
			if len(processed) != len(tc.wantQueues) {
				t.Fatalf("processed %d stage messages, want %d: %+v", len(processed), len(tc.wantQueues), processed)
			}
			for i, msg := range processed {
				if msg.TenantLane != gen.TenantLane(result.Job.TenantID) {
					t.Fatalf("message %d tenant_lane = %q, want %q", i, msg.TenantLane, gen.TenantLane(result.Job.TenantID))
				}
				if got := gen.QueueName(tc.tier, msg.ResourceClass); got != tc.wantQueues[i] {
					t.Fatalf("message %d queue = %s, want %s (stage=%s resource_class=%s)", i, got, tc.wantQueues[i], msg.Stage, msg.ResourceClass)
				}
			}
			got, err := repo.GetJob(ctx, result.Job.TenantID, result.Job.ID)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if got.Status != generation.StatusComplete || got.CurrentStage != generation.StageTerminal {
				t.Fatalf("final job state = status:%s stage:%s, want COMPLETE/TERMINAL", got.Status, got.CurrentStage)
			}
			if got.BudgetMicroUSD != tc.wantCostMicroUSD {
				t.Fatalf("budget = %d, want %d", got.BudgetMicroUSD, tc.wantCostMicroUSD)
			}
			if provider.calls.Load() != 1 {
				t.Fatalf("provider calls = %d, want 1", provider.calls.Load())
			}
			if quota.ensureCalls.Load() != 1 {
				t.Fatalf("quota Ensure calls = %d, want 1", quota.ensureCalls.Load())
			}
			if ledger.reserveCalls.Load() != 1 || ledger.commitCalls.Load() != 1 || ledger.releaseCalls.Load() != 0 {
				t.Fatalf("ledger calls reserve/commit/release = %d/%d/%d, want 1/1/0", ledger.reserveCalls.Load(), ledger.commitCalls.Load(), ledger.releaseCalls.Load())
			}
			if usage.vendorCost.Load() != tc.wantCostMicroUSD || usage.serviceCost.Load() != tc.wantCostMicroUSD || usage.generatedOutputs.Load() != 1 {
				t.Fatalf("usage vendor/service/output = %d/%d/%d, want %d/%d/1", usage.vendorCost.Load(), usage.serviceCost.Load(), usage.generatedOutputs.Load(), tc.wantCostMicroUSD, tc.wantCostMicroUSD)
			}
			if len(sink.artifacts) != 1 {
				t.Fatalf("stored artifacts = %d, want 1", len(sink.artifacts))
			}
			artifact := sink.artifacts[0]
			if !strings.HasPrefix(artifact.ContentType, tc.wantContentPrefix) {
				t.Fatalf("content type = %q, want prefix %q", artifact.ContentType, tc.wantContentPrefix)
			}
			if artifact.Extension != tc.wantExtension {
				t.Fatalf("extension = %q, want %q", artifact.Extension, tc.wantExtension)
			}
			if artifact.SHA256 == "" || len(artifact.Bytes) == 0 {
				t.Fatalf("artifact missing bytes or sha: len=%d sha=%q", len(artifact.Bytes), artifact.SHA256)
			}
			if err := gen.VerifyPublishableArtifact(artifact, tc.outputType); err != nil {
				t.Fatalf("publish gate rejected stored artifact: %v", err)
			}
			wantKey := media.StorageKey(result.Job.TenantID, result.Job.MediaID, result.Job.ResultAssetID, tc.wantExtension)
			if sink.keys[0] != wantKey {
				t.Fatalf("storage key = %q, want %q", sink.keys[0], wantKey)
			}

			if providerSubmitBody == nil {
				t.Fatal("provider submit message was not observed")
			}
			if err := runner.ProcessMessage(ctx, providerSubmitBody); err != nil {
				t.Fatalf("stale provider redelivery: %v", err)
			}
			if provider.calls.Load() != 1 {
				t.Fatalf("stale provider redelivery called provider again: %d", provider.calls.Load())
			}
			if usage.vendorCost.Load() != tc.wantCostMicroUSD || usage.serviceCost.Load() != tc.wantCostMicroUSD || usage.generatedOutputs.Load() != 1 {
				t.Fatalf("stale redelivery changed usage vendor/service/output = %d/%d/%d", usage.vendorCost.Load(), usage.serviceCost.Load(), usage.generatedOutputs.Load())
			}
		})
	}
}

func drainLifecycleQueue(t *testing.T, ctx context.Context, runner *gen.StageRunner, queue *lifecycleStageQueue, job generation.Job) ([]gen.StageMessage, []byte) {
	t.Helper()
	processed := make([]gen.StageMessage, 0, 8)
	var providerSubmitBody []byte
	for i := 0; i < 16; i++ {
		body, ok := queue.pop()
		if !ok {
			t.Fatalf("queue drained before job reached terminal")
		}
		msg := unmarshalStageMessage(t, body)
		processed = append(processed, msg)
		if msg.Stage == generation.StageProviderSubmit {
			providerSubmitBody = append([]byte(nil), body...)
		}
		if err := runner.ProcessMessage(ctx, body); err != nil {
			t.Fatalf("ProcessMessage(%s v%d): %v", msg.Stage, msg.StageVersion, err)
		}
		got, err := runner.Repo.GetJob(ctx, job.TenantID, job.ID)
		if err != nil {
			t.Fatalf("GetJob after %s: %v", msg.Stage, err)
		}
		if got.CurrentStage == generation.StageTerminal {
			return processed, providerSubmitBody
		}
	}
	t.Fatalf("queue did not reach terminal after 16 messages")
	return nil, nil
}

type lifecycleStageQueue struct {
	bodies [][]byte
}

func (q *lifecycleStageQueue) push(body []byte) {
	q.bodies = append(q.bodies, append([]byte(nil), body...))
}

func (q *lifecycleStageQueue) pop() ([]byte, bool) {
	if len(q.bodies) == 0 {
		return nil, false
	}
	body := q.bodies[0]
	q.bodies = q.bodies[1:]
	return body, true
}

type lifecycleSubmitter struct {
	repo    *gen.MemRepo
	enqueue func([]byte)
	input   gen.SubmitInput
}

func (s *lifecycleSubmitter) Submit(ctx context.Context, in gen.SubmitInput) error {
	s.input = in
	if err := s.repo.CreateJob(ctx, in.Job); err != nil {
		return err
	}
	s.enqueue(in.FirstStageBody)
	return nil
}

func deterministicIDs(ids ...string) func() string {
	next := 0
	return func() string {
		if next >= len(ids) {
			return "extra"
		}
		id := ids[next]
		next++
		return id
	}
}

type lifecycleProviderResolver struct {
	provider genprovider.Provider
}

func (r lifecycleProviderResolver) PickForJob(generation.OutputType, string) (genprovider.Provider, error) {
	return r.provider, nil
}

type countingLifecycleProvider struct {
	inner genprovider.Provider
	calls atomic.Int64
}

func (p *countingLifecycleProvider) InlineBytes() bool { return p.inner.InlineBytes() }

func (p *countingLifecycleProvider) GenerateSync(ctx context.Context, spec generation.JobSpec) (generation.Artifact, error) {
	p.calls.Add(1)
	return p.inner.GenerateSync(ctx, spec)
}

func (p *countingLifecycleProvider) SubmitAsync(ctx context.Context, spec generation.JobSpec) (string, error) {
	return p.inner.SubmitAsync(ctx, spec)
}

func (p *countingLifecycleProvider) PollAsync(ctx context.Context, providerJobID string) (generation.PollStatus, error) {
	return p.inner.PollAsync(ctx, providerJobID)
}

func (p *countingLifecycleProvider) FetchAsync(ctx context.Context, providerJobID string) (generation.Artifact, error) {
	return p.inner.FetchAsync(ctx, providerJobID)
}

func (p *countingLifecycleProvider) Name() string { return "simulated" }

func (p *countingLifecycleProvider) VendorIdempotency() genprovider.VendorIdempotencyMode {
	if declarer, ok := p.inner.(interface {
		VendorIdempotency() genprovider.VendorIdempotencyMode
	}); ok {
		return declarer.VendorIdempotency()
	}
	return genprovider.VendorIdempotencyBestEffort
}

type recordingLifecycleSink struct {
	artifacts []generation.Artifact
	keys      []string
}

func (s *recordingLifecycleSink) StoreFinalArtifact(_ context.Context, j generation.Job, art generation.Artifact) (string, error) {
	s.artifacts = append(s.artifacts, art)
	s.keys = append(s.keys, media.StorageKey(j.TenantID, j.MediaID, j.ResultAssetID, art.Extension))
	return j.ResultAssetID, nil
}

type recordingLifecycleQuota struct {
	ensureCalls atomic.Int64
}

func (q *recordingLifecycleQuota) Ensure(context.Context, string, string) error {
	q.ensureCalls.Add(1)
	return nil
}

func (q *recordingLifecycleQuota) Reserve(context.Context, string, string, int64) (bool, int64, error) {
	return true, 0, nil
}

func (q *recordingLifecycleQuota) Commit(context.Context, string, string, int64) error {
	return nil
}

func (q *recordingLifecycleQuota) Release(context.Context, string, string, int64) error {
	return nil
}

type recordingLifecycleLedger struct {
	reserveCalls atomic.Int64
	commitCalls  atomic.Int64
	releaseCalls atomic.Int64
}

func (l *recordingLifecycleLedger) LedgerPutReserved(string, string, string, int64, int) gen.LedgerOp {
	l.reserveCalls.Add(1)
	return gen.LedgerOp{}
}

func (l *recordingLifecycleLedger) LedgerUpdateCommitted(string, string, string, int64) gen.LedgerOp {
	l.commitCalls.Add(1)
	return gen.LedgerOp{}
}

func (l *recordingLifecycleLedger) LedgerUpdateReleased(string, string, string, int64) gen.LedgerOp {
	l.releaseCalls.Add(1)
	return gen.LedgerOp{}
}

type recordingLifecycleUsage struct {
	generatedOutputs atomic.Int64
	vendorCost       atomic.Int64
	serviceCost      atomic.Int64
}

func (u *recordingLifecycleUsage) RecordGeneratedOutput(context.Context, string, string, string) error {
	u.generatedOutputs.Add(1)
	return nil
}

func (u *recordingLifecycleUsage) RecordVendorCost(_ context.Context, _ string, _ string, microUSD int64) error {
	u.vendorCost.Add(microUSD)
	return nil
}

func (u *recordingLifecycleUsage) RecordServiceCost(_ context.Context, _ string, microUSD int64) error {
	u.serviceCost.Add(microUSD)
	return nil
}

type lifecyclePromptSealer struct{}

func (lifecyclePromptSealer) Seal(_ context.Context, _, _ string, plaintext string) ([]byte, error) {
	return []byte("sealed:" + plaintext), nil
}

func (lifecyclePromptSealer) Unseal(_ context.Context, _, _ string, ciphertext []byte) (string, error) {
	return strings.TrimPrefix(string(ciphertext), "sealed:"), nil
}
