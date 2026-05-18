package generation

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

func TestSubmissionReplayBypassesBudgetHintAndReturnsPersistedJob(t *testing.T) {
	ctx := context.Background()
	cmd := testSubmitCommand()
	persisted := &domaingen.Job{
		ID:           "gen_replay",
		TenantID:     cmd.TenantID,
		MediaID:      "med_replay",
		OutputType:   cmd.OutputType,
		Tier:         cmd.Tier,
		Status:       domaingen.StatusComplete,
		CurrentStage: domaingen.StageTerminal,
		StageVersion: 8,
	}
	submitter := &recordingSubmitter{}
	hint := &recordingCapacityHint{ok: false}
	svc := SubmissionService{
		Submitter:    submitter,
		ReplayReader: jobReaderFunc(func(context.Context, string, string) (*domaingen.Job, error) { return persisted, nil }),
		Idempotency: recordingIdempotency{
			ref:        "gen_replay:med_replay",
			inputHash:  submitInputHash(cmd),
			status:     idempotency.StatusCompleted,
			resultErr:  nil,
			returnedOK: true,
		},
		CapacityHint: hint,
		NewID:        func() string { t.Fatal("NewID called on replay"); return "" },
	}

	result, err := svc.Create(ctx, cmd)
	if err != nil {
		t.Fatalf("Create replay: %v", err)
	}
	if !result.Replay {
		t.Fatal("Replay=false, want true")
	}
	if result.Job.Status != domaingen.StatusComplete || result.Job.CurrentStage != domaingen.StageTerminal || result.Job.StageVersion != 8 {
		t.Fatalf("replay job = %+v, want persisted terminal state", result.Job)
	}
	if hint.calls != 0 {
		t.Fatalf("budget hint calls = %d, want 0", hint.calls)
	}
	if submitter.calls != 0 {
		t.Fatalf("submit calls = %d, want 0", submitter.calls)
	}
}

func TestSubmissionBudgetHintRejectsBeforeSubmit(t *testing.T) {
	ctx := context.Background()
	cmd := testSubmitCommand()
	submitter := &recordingSubmitter{}
	hint := &recordingCapacityHint{ok: false, available: 3_999}
	reader := sdkmetric.NewManualReader()
	inst := testInstruments(t, reader)
	svc := SubmissionService{
		Submitter:    submitter,
		Idempotency:  recordingIdempotency{resultErr: errors.New("not found")},
		CapacityHint: hint,
		Instruments:  inst,
		Now:          func() time.Time { return time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC) },
		NewID:        func() string { t.Fatal("NewID called after rejected hint"); return "" },
		ReplayReader: nil,
	}

	_, err := svc.Create(ctx, cmd)
	if !errors.Is(err, ErrBudgetInsufficient) {
		t.Fatalf("Create err = %v, want ErrBudgetInsufficient", err)
	}
	if submitter.calls != 0 {
		t.Fatalf("submit calls = %d, want 0", submitter.calls)
	}
	if hint.period != "20260518" {
		t.Fatalf("hint period = %s, want 20260518", hint.period)
	}
	metrics := collectSubmissionMetrics(t, reader)
	if got := metrics["generation.budget_preflight_total|outcome=rejected|output_type=IMAGE|tier=FREE"]; got != 1 {
		t.Fatalf("rejected preflight metric = %d, want 1; all=%v", got, metrics)
	}
	if got := metrics["generation.submit_rejected_total|output_type=IMAGE|reason=BUDGET_INSUFFICIENT|tier=FREE"]; got != 1 {
		t.Fatalf("submit rejected metric = %d, want 1; all=%v", got, metrics)
	}
}

func TestSubmissionBudgetHintErrorFailsOpen(t *testing.T) {
	ctx := context.Background()
	cmd := testSubmitCommand()
	submitter := &recordingSubmitter{}
	reader := sdkmetric.NewManualReader()
	inst := testInstruments(t, reader)
	nextID := deterministicSubmitIDs("media", "job", "asset")
	svc := SubmissionService{
		Submitter:   submitter,
		Idempotency: recordingIdempotency{resultErr: errors.New("not found")},
		CapacityHint: &recordingCapacityHint{
			err: errors.New("ddb throttled"),
		},
		Instruments: inst,
		Now:         func() time.Time { return time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC) },
		NewID:       nextID,
	}

	result, err := svc.Create(ctx, cmd)
	if err != nil {
		t.Fatalf("Create fail-open: %v", err)
	}
	if result.Job.ID != "gen_job" || submitter.calls != 1 {
		t.Fatalf("job id/calls = %s/%d, want gen_job/1", result.Job.ID, submitter.calls)
	}
	metrics := collectSubmissionMetrics(t, reader)
	if got := metrics["generation.budget_preflight_total|outcome=error_fail_open|output_type=IMAGE|tier=FREE"]; got != 1 {
		t.Fatalf("fail-open metric = %d, want 1; all=%v", got, metrics)
	}
}

func TestBudgetEstimateSharedBySubmitAndReserve(t *testing.T) {
	cmd := testSubmitCommand()
	job := domaingen.Job{
		OutputType:   cmd.OutputType,
		Provider:     cmd.Provider,
		Model:        cmd.Model,
		Resolution:   cmd.ResolutionLabel,
		VariantCount: submitVariantCount,
		Tier:         cmd.Tier,
	}
	if got, want := RequiredBudgetMicroUSD(BudgetEstimateFromSubmit(cmd)), RequiredBudgetMicroUSD(BudgetEstimateFromJob(job)); got != want {
		t.Fatalf("submit/reserve required budget = %d/%d", got, want)
	}
}

func TestStageQuotaReserveUsesJobCreatedAtPeriod(t *testing.T) {
	ctx := context.Background()
	quota := &recordingQuotaReserver{granted: true}
	w := &Workflow{QuotaReserver: quota, Clock: func() time.Time {
		return time.Date(2026, 5, 19, 1, 0, 0, 0, time.UTC)
	}}
	job := &domaingen.Job{
		ID:           "gen_budget_period",
		TenantID:     "tenant-period",
		OutputType:   domaingen.OutputImage,
		Tier:         domaingen.TierFree,
		CurrentStage: domaingen.StageCostReserve,
		StageVersion: 2,
		Provider:     "simulated",
		Model:        "simulated-v1",
		VariantCount: 1,
		CreatedAt:    time.Date(2026, 5, 18, 23, 59, 55, 0, time.UTC),
	}

	result, err := w.stageQuotaReserve(ctx, job)
	if err != nil {
		t.Fatalf("stageQuotaReserve: %v", err)
	}
	if quota.reservePeriod != "20260518" || result.BudgetDate != "20260518" {
		t.Fatalf("reserve/result period = %s/%s, want 20260518", quota.reservePeriod, result.BudgetDate)
	}
	if result.BudgetMicroUSD != RequiredBudgetMicroUSD(BudgetEstimateFromJob(*job)) {
		t.Fatalf("budget micro USD = %d", result.BudgetMicroUSD)
	}
}

func testSubmitCommand() SubmitCommand {
	return SubmitCommand{
		TenantID:        "tenant-submit",
		UserID:          "user-submit",
		Prompt:          "draw a budget preflight test",
		Provider:        "simulated",
		Model:           "simulated-v1",
		OutputType:      domaingen.OutputImage,
		Tier:            domaingen.TierFree,
		ResolutionLabel: "1024x1024",
		Seed:            9,
		IdempotencyKey:  "idem-submit",
	}
}

type recordingSubmitter struct {
	calls int
	input SubmitInput
	err   error
}

func (s *recordingSubmitter) Submit(_ context.Context, in SubmitInput) error {
	s.calls++
	s.input = in
	return s.err
}

type recordingIdempotency struct {
	ref        string
	inputHash  string
	status     idempotency.Status
	resultErr  error
	returnedOK bool
}

func (i recordingIdempotency) GetResultWithHash(context.Context, string) (string, string, idempotency.Status, error) {
	if i.resultErr != nil {
		return "", "", "", i.resultErr
	}
	if !i.returnedOK {
		return "", "", "", errors.New("not found")
	}
	return i.ref, i.inputHash, i.status, nil
}

type recordingCapacityHint struct {
	calls     int
	ok        bool
	available int64
	err       error
	period    string
	required  int64
}

func (h *recordingCapacityHint) HasCapacity(_ context.Context, _ string, period string, requiredMicroUSD int64) (bool, int64, error) {
	h.calls++
	h.period = period
	h.required = requiredMicroUSD
	return h.ok, h.available, h.err
}

type jobReaderFunc func(context.Context, string, string) (*domaingen.Job, error)

func (f jobReaderFunc) GetJob(ctx context.Context, tenantID, jobID string) (*domaingen.Job, error) {
	return f(ctx, tenantID, jobID)
}

type recordingQuotaReserver struct {
	granted       bool
	reservePeriod string
}

func (q *recordingQuotaReserver) Ensure(context.Context, string, string) error { return nil }

func (q *recordingQuotaReserver) Reserve(_ context.Context, _ string, period string, _ int64) (bool, int64, error) {
	q.reservePeriod = period
	return q.granted, 0, nil
}

func (q *recordingQuotaReserver) Commit(context.Context, string, string, int64) error {
	return nil
}

func (q *recordingQuotaReserver) Release(context.Context, string, string, int64) error {
	return nil
}

func deterministicSubmitIDs(ids ...string) func() string {
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

func testInstruments(t *testing.T, reader *sdkmetric.ManualReader) *obs.Instruments {
	t.Helper()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := obs.NewInstruments(mp.Meter(obs.MeterName))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return inst
}

func collectSubmissionMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "generation.budget_preflight_total" && m.Name != "generation.submit_rejected_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				out[submissionMetricKey(m.Name, dp.Attributes.ToSlice())] += dp.Value
			}
		}
	}
	return out
}

func submissionMetricKey(name string, attrs []attribute.KeyValue) string {
	parts := map[string]string{}
	for _, attr := range attrs {
		parts[string(attr.Key)] = attr.Value.AsString()
	}
	keys := []string{"outcome", "output_type", "reason", "tier"}
	out := name
	for _, key := range keys {
		if parts[key] != "" {
			out += "|" + key + "=" + parts[key]
		}
	}
	return out
}
