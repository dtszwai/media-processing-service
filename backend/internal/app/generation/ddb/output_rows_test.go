package ddb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestFailGenerationOutputOpsMarksGenerationAndOutputFailed(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	job := generation.Job{ID: "gen_test"}
	terminalErr := &generation.Error{Code: "PROVIDER_UNAVAILABLE", Message: "codex not registered on this worker", Terminal: true}

	ops := failGenerationOutputOps(job, terminalErr, now)

	if len(ops) != 2 {
		t.Fatalf("len(ops) = %d, want 2", len(ops))
	}
	assertFailedOutputUpdate(t, ops[0].Update, GenerationSK(), terminalErr, now)
	assertFailedOutputUpdate(t, ops[1].Update, OutputSK(OutputID(job.ID)), terminalErr, now)
}

func TestCancelGenerationOutputOpsMarksGenerationAndOutputCancelled(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	job := generation.Job{ID: "gen_test"}
	ops := cancelGenerationOutputOps(job, "operator stopped job", now)

	if len(ops) != 2 {
		t.Fatalf("len(ops) = %d, want 2", len(ops))
	}
	for _, op := range ops {
		if op.Update.ExpressionAttributeValues[":status"] != string(generation.StatusCancelled) {
			t.Fatalf(":status = %v, want CANCELLED", op.Update.ExpressionAttributeValues[":status"])
		}
		if op.Update.ExpressionAttributeValues[":code"] != "CANCELLED" {
			t.Fatalf(":code = %v, want CANCELLED", op.Update.ExpressionAttributeValues[":code"])
		}
		if op.Update.ExpressionAttributeValues[":msg"] != "operator stopped job" {
			t.Fatalf(":msg = %v, want cancellation reason", op.Update.ExpressionAttributeValues[":msg"])
		}
	}
}

func TestGenerationOutputVariantIDsTrimExistingGenerationPrefix(t *testing.T) {
	jobID := "gen_existing"

	if got := GenerationID(jobID); got != "gen_existing" {
		t.Fatalf("GenerationID(%q) = %q, want gen_existing", jobID, got)
	}
	if got := OutputID(jobID); got != "out_existing" {
		t.Fatalf("OutputID(%q) = %q, want out_existing", jobID, got)
	}
	if got := VariantID(jobID, 0); got != "var_existing_0" {
		t.Fatalf("VariantID(%q, 0) = %q, want var_existing_0", jobID, got)
	}
}

func TestAdvanceStageAndEnqueueFailsOutputRecordOnTerminalError(t *testing.T) {
	rec := &recordingKV{}
	repo := NewJobRepo(rec, nil)
	job := &generation.Job{
		ID:           "gen_terminal_fail",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusRunning,
		CurrentStage: generation.StageProviderSubmit,
		StageVersion: 1,
		OutputType:   generation.OutputImage,
	}
	terminalErr := &generation.Error{Code: "PROVIDER_UNAVAILABLE", Message: "codex not registered on this worker", Terminal: true}

	err := repo.AdvanceStageAndEnqueue(context.Background(), job, genapp.StageResult{
		NextStage:     genapp.StageTerminal,
		ResourceClass: generation.ResourceFast,
		TerminalError: terminalErr,
	})
	if err != nil {
		t.Fatalf("AdvanceStageAndEnqueue: %v", err)
	}

	assertRecordedFailureUpdate(t, rec.ops, GenerationSK(), terminalErr)
	assertRecordedFailureUpdate(t, rec.ops, OutputSK(OutputID(job.ID)), terminalErr)
}

func TestAdvanceStageAndEnqueueWritesGateAuditEventWithStageTransaction(t *testing.T) {
	rec := &recordingKV{}
	repo := NewJobRepo(rec, nil)
	job := &generation.Job{
		ID:           "gen_gate_audit",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusRunning,
		CurrentStage: generation.StageDisclosurePostprocess,
		StageVersion: 7,
		OutputType:   generation.OutputImage,
		Provider:     "simulated",
		Model:        "test-model",
	}

	err := repo.AdvanceStageAndEnqueue(context.Background(), job, genapp.StageResult{
		NextStage:     generation.StagePublish,
		ResourceClass: generation.ResourceFast,
		GateDecision: &genapp.GateDecision{
			JobID:             job.ID,
			TenantID:          job.TenantID,
			GateVersion:       "v1",
			OutputType:        string(job.OutputType),
			Provider:          job.Provider,
			Model:             job.Model,
			Decision:          "PASS",
			WatermarkPresent:  true,
			DisclosurePresent: true,
			SafetyPresent:     true,
		},
	})
	if err != nil {
		t.Fatalf("AdvanceStageAndEnqueue: %v", err)
	}

	if !containsPutItem(rec.ops, "event_type", "safety.disclosure_gate.decided") {
		t.Fatalf("missing audit-wide disclosure gate event in %d ops", len(rec.ops))
	}
	if !containsPutItem(rec.ops, "decision", "PASS") {
		t.Fatalf("missing PASS gate decision row in %d ops", len(rec.ops))
	}
}

func TestAdvanceStageAndEnqueueWritesWorkflowAuditEvents(t *testing.T) {
	rec := &recordingKV{}
	repo := NewJobRepo(rec, nil)
	job := &generation.Job{
		ID:           "gen_prompt_audit",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusRunning,
		CurrentStage: generation.StagePromptPrepare,
		StageVersion: 1,
		OutputType:   generation.OutputImage,
	}
	applied := true
	err := repo.AdvanceStageAndEnqueue(context.Background(), job, genapp.StageResult{
		NextStage:                generation.StageProviderSubmit,
		ResourceClass:            generation.ResourceProvider,
		PromptEnhancementApplied: &applied,
		PromptEnhancementRef:     "enh_123",
		AuditEvents: []audit.Event{auditapp.NewWorkflowPromptEnhancementApplied(
			job.TenantID, job.ID, true, "enh_123", "policy-v1", "openai", "gpt-test", "IMAGE", 1, 2,
		)},
	})
	if err != nil {
		t.Fatalf("AdvanceStageAndEnqueue: %v", err)
	}
	if !containsPutItem(rec.ops, "event_type", audit.EventWorkflowPromptEnhancementApplied) {
		t.Fatalf("missing prompt enhancement audit event in %d ops", len(rec.ops))
	}
	update := rec.ops[0].Update
	if update == nil || update.ExpressionAttributeValues[":pea"] != true || update.ExpressionAttributeValues[":per"] != "enh_123" {
		t.Fatalf("job update missing prompt enhancement fields: %#v", update)
	}
}

// Non-gate terminal failures (RETRY_EXHAUSTED, BUDGET_EXHAUSTED, etc.) must
// not emit an AUDIT#GATE row. The row is only written when result.GateDecision
// is set (i.e. the gate actually ran).
func TestAdvanceStageAndEnqueueOmitsGateAuditRowForNonGateTerminal(t *testing.T) {
	rec := &recordingKV{}
	repo := NewJobRepo(rec, nil)
	job := &generation.Job{
		ID:           "gen_terminal_no_gate",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusRunning,
		CurrentStage: generation.StageProviderSubmit,
		StageVersion: 6,
		OutputType:   generation.OutputImage,
		Model:        "gpt-5.5",
	}
	terminalErr := &generation.Error{Code: "RETRY_EXHAUSTED", Message: "UNKNOWN_ERROR: context deadline exceeded", Terminal: true}

	err := repo.AdvanceStageAndEnqueue(context.Background(), job, genapp.StageResult{
		NextStage:     genapp.StageTerminal,
		ResourceClass: generation.ResourceProvider,
		TerminalError: terminalErr,
	})
	if err != nil {
		t.Fatalf("AdvanceStageAndEnqueue: %v", err)
	}

	for _, op := range rec.ops {
		if op.Put == nil {
			continue
		}
		item, ok := op.Put.Item.(map[string]any)
		if !ok {
			continue
		}
		if pk, _ := item["PK"].(string); strings.HasPrefix(pk, "AUDIT#GATE#") {
			t.Fatalf("terminal failure without GateDecision wrote AUDIT#GATE row: %v", item)
		}
		if event, _ := item["event_type"].(string); event == "safety.disclosure_gate.decided" {
			t.Fatalf("terminal failure without GateDecision wrote disclosure-gate event: %v", item)
		}
	}
}

func TestAdvanceStageAndEnqueueClassifiesLedgerReserveExhaustion(t *testing.T) {
	reasons := []kv.ItemCancelReason{
		{Code: "None"},
		{Code: "None"},
		{Code: "None"},
		{ConditionFailed: true, Code: "ConditionalCheckFailed"},
		{Code: "None"},
	}
	rec := &recordingKV{err: &fakeTxnErr{items: reasons}}
	repo := NewJobRepo(rec, nil)
	job := &generation.Job{
		ID:           "gen_budget_exhausted",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusRunning,
		CurrentStage: generation.StageCostReserve,
		StageVersion: 1,
		OutputType:   generation.OutputImage,
	}

	err := repo.AdvanceStageAndEnqueue(context.Background(), job, genapp.StageResult{
		NextStage:     generation.StagePromptPrepare,
		OutboxBody:    []byte(`{"stage":"PROMPT_PREPARE"}`),
		ResourceClass: generation.ResourceFast,
		LedgerOp: &genapp.LedgerOp{Items: []kv.WriteOp{
			{Update: &kv.UpdateOp{}},
			{Put: &kv.PutOp{}},
		}},
	})

	if !generation.IsTerminal(err) {
		t.Fatalf("err = %v, want terminal BUDGET_EXHAUSTED", err)
	}
	if got := generation.AsError(err).Code; got != "BUDGET_EXHAUSTED" {
		t.Fatalf("code = %q, want BUDGET_EXHAUSTED", got)
	}
}

func TestLedgerOpNamesDistinguishReserveCommitRelease(t *testing.T) {
	tests := []struct {
		name          string
		jobStage      generation.Stage
		result        genapp.StageResult
		wantAggregate kv.TxOpName
		wantLedger    kv.TxOpName
	}{
		{
			name:          "reserve",
			jobStage:      generation.StageCostReserve,
			result:        genapp.StageResult{NextStage: generation.StagePromptPrepare},
			wantAggregate: "aggregate_reserve",
			wantLedger:    "ledger_reserve",
		},
		{
			name:          "commit",
			jobStage:      generation.StageProviderSubmit,
			result:        genapp.StageResult{NextStage: generation.StageOutputModeration},
			wantAggregate: "aggregate_commit",
			wantLedger:    "ledger_commit",
		},
		{
			name:     "release",
			jobStage: generation.StagePromptPrepare,
			result: genapp.StageResult{
				NextStage:     genapp.StageTerminal,
				TerminalError: &generation.Error{Code: "BLOCKED", Terminal: true},
			},
			wantAggregate: "aggregate_release",
			wantLedger:    "ledger_release",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAggregate, gotLedger := ledgerOpNames(&generation.Job{CurrentStage: tt.jobStage}, tt.result)
			if gotAggregate != tt.wantAggregate || gotLedger != tt.wantLedger {
				t.Fatalf("ledgerOpNames = (%s, %s), want (%s, %s)", gotAggregate, gotLedger, tt.wantAggregate, tt.wantLedger)
			}
		})
	}
}

func TestCompleteGenerationOutputOpsAllowsSameAssetVariantReplay(t *testing.T) {
	ops := completeGenerationOutputOps(generation.Job{ID: "gen_replay"}, "asset-final", generation.Artifact{}, time.Now().UTC())

	put := ops[0].Put
	if put == nil {
		t.Fatal("variant op is not a Put")
	}
	if put.ConditionExpression != "attribute_not_exists(PK) OR final_asset_id = :asset_id" {
		t.Fatalf("variant condition = %q, want same-asset replay guard", put.ConditionExpression)
	}
	if put.ExpressionAttributeValues[":asset_id"] != "asset-final" {
		t.Fatalf(":asset_id = %v, want asset-final", put.ExpressionAttributeValues[":asset_id"])
	}
}

func TestBuildStageAttemptItemStoresTraceContext(t *testing.T) {
	row := buildStageAttemptItem(&generation.Job{
		ID:           "gen_trace",
		TenantID:     "tenant-test",
		CurrentStage: generation.StageInputModeration,
		StageVersion: 1,
	}, genapp.StageResult{
		NextStage:     generation.StageCostReserve,
		ResourceClass: generation.ResourceFast,
	}, time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC), "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	if row["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent = %v, want persisted traceparent", row["traceparent"])
	}
	if row["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id = %v, want parsed trace id", row["trace_id"])
	}
}

func assertFailedOutputUpdate(t *testing.T, op *kv.UpdateOp, sk string, terminalErr *generation.Error, now time.Time) {
	t.Helper()
	if op == nil {
		t.Fatalf("update op for %s is nil", sk)
	}
	if op.Key.SK != sk {
		t.Fatalf("SK = %q, want %q", op.Key.SK, sk)
	}
	if op.ExpressionAttributeValues[":status"] != string(generation.StatusFailed) {
		t.Fatalf(":status = %v, want FAILED", op.ExpressionAttributeValues[":status"])
	}
	if op.ExpressionAttributeValues[":now"] != now.Format(time.RFC3339Nano) {
		t.Fatalf(":now = %v, want %s", op.ExpressionAttributeValues[":now"], now.Format(time.RFC3339Nano))
	}
	if op.ExpressionAttributeValues[":code"] != terminalErr.Code {
		t.Fatalf(":code = %v, want %s", op.ExpressionAttributeValues[":code"], terminalErr.Code)
	}
	if op.ExpressionAttributeValues[":msg"] != terminalErr.Message {
		t.Fatalf(":msg = %v, want %s", op.ExpressionAttributeValues[":msg"], terminalErr.Message)
	}
}

func assertRecordedFailureUpdate(t *testing.T, ops []kv.WriteOp, sk string, terminalErr *generation.Error) {
	t.Helper()
	for _, op := range ops {
		if op.Update == nil || op.Update.Key.SK != sk {
			continue
		}
		if op.Update.ExpressionAttributeValues[":status"] == string(generation.StatusFailed) &&
			op.Update.ExpressionAttributeValues[":code"] == terminalErr.Code &&
			op.Update.ExpressionAttributeValues[":msg"] == terminalErr.Message {
			return
		}
	}
	t.Fatalf("missing failed update for SK %s in %d ops", sk, len(ops))
}

func containsPutItem(ops []kv.WriteOp, key string, want any) bool {
	for _, op := range ops {
		if op.Put == nil {
			continue
		}
		item, ok := op.Put.Item.(map[string]any)
		if !ok {
			continue
		}
		if item[key] == want {
			return true
		}
	}
	return false
}

type recordingKV struct {
	ops    []kv.WriteOp
	err    error
	jobRow *jobRow
	getErr error
}

func (r *recordingKV) Put(context.Context, kv.Item, kv.PutOptions) error {
	return errors.New("recordingKV: Put not supported")
}

func (r *recordingKV) Get(_ context.Context, _ kv.Key, out any) error {
	if r.getErr != nil {
		return r.getErr
	}
	if r.jobRow != nil {
		dst, ok := out.(*jobRow)
		if !ok {
			return errors.New("recordingKV: unsupported Get destination")
		}
		*dst = *r.jobRow
		return nil
	}
	return errors.New("recordingKV: Get not supported")
}

func (r *recordingKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("recordingKV: Query not supported")
}

func (r *recordingKV) Update(context.Context, kv.UpdateOp) error {
	return errors.New("recordingKV: Update not supported")
}

func (r *recordingKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("recordingKV: UpdateReturning not supported")
}

func (r *recordingKV) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("recordingKV: Delete not supported")
}

func (r *recordingKV) TransactWrite(_ context.Context, ops []kv.WriteOp) error {
	r.ops = append([]kv.WriteOp(nil), ops...)
	return r.err
}

type fakeTxnErr struct {
	items []kv.ItemCancelReason
}

func (e *fakeTxnErr) Error() string                { return "txn cancelled" }
func (e *fakeTxnErr) Items() []kv.ItemCancelReason { return e.items }
