package ddb

import (
	"context"
	"strings"
	"testing"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestForceFailJobTransactsTerminalSideEffects(t *testing.T) {
	job := generation.Job{
		ID:           "gen_force_fail",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusRunning,
		CurrentStage: generation.StageProviderSubmit,
		StageVersion: 4,
	}
	rec := &recordingKV{jobRow: ptrJobRow(job)}
	repo := NewJobRepo(rec, nil)
	terminalErr := &generation.Error{Code: "OPERATOR_FORCED_FAIL", Message: "operator failed job", Terminal: true}

	if err := repo.ForceFailJob(context.Background(), job.TenantID, job.ID, terminalErr.Code, terminalErr.Message); err != nil {
		t.Fatalf("ForceFailJob: %v", err)
	}

	if len(rec.ops) != 5 {
		t.Fatalf("len(ops) = %d, want 5", len(rec.ops))
	}
	jobUpdate := rec.ops[0].Update
	if jobUpdate == nil {
		t.Fatal("first op is not job update")
	}
	if jobUpdate.Key.PK != JobPK(job.ID) || jobUpdate.Key.SK != JobSK {
		t.Fatalf("job update key = %#v, want job key", jobUpdate.Key)
	}
	if !strings.Contains(jobUpdate.ConditionExpression, "#st IN (:queued, :running, :blocked)") {
		t.Fatalf("condition = %q, want active-status guard", jobUpdate.ConditionExpression)
	}
	if jobUpdate.ExpressionAttributeValues[":v"] != job.StageVersion {
		t.Fatalf(":v = %v, want %d", jobUpdate.ExpressionAttributeValues[":v"], job.StageVersion)
	}
	if jobUpdate.ExpressionAttributeValues[":nv"] != job.StageVersion+1 {
		t.Fatalf(":nv = %v, want %d", jobUpdate.ExpressionAttributeValues[":nv"], job.StageVersion+1)
	}
	if jobUpdate.ExpressionAttributeValues[":failed"] != string(generation.StatusFailed) {
		t.Fatalf(":failed = %v, want FAILED", jobUpdate.ExpressionAttributeValues[":failed"])
	}
	if jobUpdate.ExpressionAttributeValues[":terminal"] != string(generation.StageTerminal) {
		t.Fatalf(":terminal = %v, want TERMINAL", jobUpdate.ExpressionAttributeValues[":terminal"])
	}
	if jobUpdate.ExpressionAttributeValues[":code"] != terminalErr.Code {
		t.Fatalf(":code = %v, want %s", jobUpdate.ExpressionAttributeValues[":code"], terminalErr.Code)
	}
	if jobUpdate.ExpressionAttributeValues[":message"] != terminalErr.Message {
		t.Fatalf(":message = %v, want %s", jobUpdate.ExpressionAttributeValues[":message"], terminalErr.Message)
	}

	mediaUpdate := rec.ops[1].Update
	if mediaUpdate == nil {
		t.Fatal("second op is not media update")
	}
	wantMediaKey := mediaapp.MediaKey(job.TenantID, job.MediaID)
	if mediaUpdate.Key.PK != wantMediaKey.PK || mediaUpdate.Key.SK != wantMediaKey.SK {
		t.Fatalf("media update key = %#v, want media key", mediaUpdate.Key)
	}
	if mediaUpdate.ExpressionAttributeValues[":media_lifecycle"] != string(media.LifecycleFailed) {
		t.Fatalf(":media_lifecycle = %v, want FAILED", mediaUpdate.ExpressionAttributeValues[":media_lifecycle"])
	}

	terminalPut := rec.ops[2].Put
	if terminalPut == nil {
		t.Fatal("third op is not terminal put")
	}
	terminalItem, ok := terminalPut.Item.(map[string]any)
	if !ok {
		t.Fatalf("terminal item has type %T, want map[string]any", terminalPut.Item)
	}
	if terminalItem["SK"] != TerminalSK {
		t.Fatalf("terminal SK = %v, want %s", terminalItem["SK"], TerminalSK)
	}
	if terminalItem["status"] != string(generation.StatusFailed) {
		t.Fatalf("terminal status = %v, want FAILED", terminalItem["status"])
	}
	if terminalItem["error_code"] != terminalErr.Code {
		t.Fatalf("terminal error_code = %v, want %s", terminalItem["error_code"], terminalErr.Code)
	}
	if terminalItem["error_message"] != terminalErr.Message {
		t.Fatalf("terminal error_message = %v, want %s", terminalItem["error_message"], terminalErr.Message)
	}

	assertRecordedFailureUpdate(t, rec.ops, GenerationSK(), terminalErr)
	assertRecordedFailureUpdate(t, rec.ops, OutputSK(OutputID(job.ID)), terminalErr)
}

func TestForceFailJobRejectsTerminalJob(t *testing.T) {
	job := generation.Job{
		ID:           "gen_already_complete",
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		Status:       generation.StatusComplete,
		CurrentStage: generation.StageTerminal,
		StageVersion: 9,
	}
	rec := &recordingKV{jobRow: ptrJobRow(job)}
	repo := NewJobRepo(rec, nil)

	err := repo.ForceFailJob(context.Background(), job.TenantID, job.ID, "OPERATOR_FORCED_FAIL", "operator failed job")

	if err == nil {
		t.Fatal("ForceFailJob succeeded, want already-terminal error")
	}
	if !strings.Contains(err.Error(), "job already terminal") {
		t.Fatalf("err = %v, want already-terminal error", err)
	}
	if len(rec.ops) != 0 {
		t.Fatalf("len(ops) = %d, want no transaction", len(rec.ops))
	}
}

func TestForceFailJobReleasesReservedQuotaBeforeProviderCommit(t *testing.T) {
	job := generation.Job{
		ID:             "gen_force_fail_reserved",
		TenantID:       "tenant-test",
		MediaID:        "media-test",
		Status:         generation.StatusRunning,
		CurrentStage:   generation.StageProviderWait,
		StageVersion:   4,
		BudgetDate:     "20260517",
		BudgetMicroUSD: 25000,
	}
	rec := &recordingKV{jobRow: ptrJobRow(job)}
	ledger := &recordingQuotaLedger{}
	repo := NewJobRepo(rec, nil)
	repo.QuotaLedger = ledger

	if err := repo.ForceFailJob(context.Background(), job.TenantID, job.ID, "OPERATOR_FORCED_FAIL", "operator failed job"); err != nil {
		t.Fatalf("ForceFailJob: %v", err)
	}

	if len(rec.ops) != 7 {
		t.Fatalf("len(ops) = %d, want terminal ops plus quota release", len(rec.ops))
	}
	assertLedgerReleaseCall(t, ledger, job)
	assertReleaseOpsAppended(t, rec.ops)
}

func TestForceFailJobDoesNotReleaseQuotaAfterProviderCommit(t *testing.T) {
	job := generation.Job{
		ID:             "gen_force_fail_post_commit",
		TenantID:       "tenant-test",
		MediaID:        "media-test",
		Status:         generation.StatusRunning,
		CurrentStage:   generation.StageOutputModeration,
		StageVersion:   5,
		BudgetDate:     "20260517",
		BudgetMicroUSD: 25000,
	}
	rec := &recordingKV{jobRow: ptrJobRow(job)}
	ledger := &recordingQuotaLedger{}
	repo := NewJobRepo(rec, nil)
	repo.QuotaLedger = ledger

	if err := repo.ForceFailJob(context.Background(), job.TenantID, job.ID, "OPERATOR_FORCED_FAIL", "operator failed job"); err != nil {
		t.Fatalf("ForceFailJob: %v", err)
	}

	if len(rec.ops) != 5 {
		t.Fatalf("len(ops) = %d, want no quota release after commit boundary", len(rec.ops))
	}
	if ledger.releaseCalls != 0 {
		t.Fatalf("releaseCalls = %d, want 0", ledger.releaseCalls)
	}
}

func TestCancelJobReleasesReservedQuotaBeforeProviderCommit(t *testing.T) {
	job := generation.Job{
		ID:             "gen_cancel_reserved",
		TenantID:       "tenant-test",
		MediaID:        "media-test",
		Status:         generation.StatusRunning,
		CurrentStage:   generation.StagePromptPrepare,
		StageVersion:   2,
		BudgetDate:     "20260517",
		BudgetMicroUSD: 25000,
	}
	rec := &recordingKV{jobRow: ptrJobRow(job)}
	ledger := &recordingQuotaLedger{}
	repo := NewJobRepo(rec, nil)
	repo.QuotaLedger = ledger

	if err := repo.CancelJob(context.Background(), job.TenantID, job.ID, "operator cancel"); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}

	if len(rec.ops) != 7 {
		t.Fatalf("len(ops) = %d, want terminal ops plus quota release", len(rec.ops))
	}
	assertLedgerReleaseCall(t, ledger, job)
	assertReleaseOpsAppended(t, rec.ops)
}

type recordingQuotaLedger struct {
	releaseCalls int
	tenantID     string
	jobID        string
	period       string
	amount       int64
}

func (l *recordingQuotaLedger) LedgerPutReserved(string, string, string, int64, int) genapp.LedgerOp {
	return genapp.LedgerOp{}
}

func (l *recordingQuotaLedger) LedgerUpdateCommitted(string, string, string, int64) genapp.LedgerOp {
	return genapp.LedgerOp{}
}

func (l *recordingQuotaLedger) LedgerUpdateReleased(tenantID, jobID, period string, amount int64) genapp.LedgerOp {
	l.releaseCalls++
	l.tenantID = tenantID
	l.jobID = jobID
	l.period = period
	l.amount = amount
	return genapp.LedgerOp{Items: []kv.WriteOp{
		{Update: &kv.UpdateOp{Key: kv.Key{PK: "AGGREGATE_RELEASE", SK: "QUOTA"}}},
		{Update: &kv.UpdateOp{Key: kv.Key{PK: "LEDGER_RELEASE", SK: "QUOTA"}}},
	}}
}

func ptrJobRow(job generation.Job) *jobRow {
	row := jobRowFromDomain(job)
	return &row
}

func assertLedgerReleaseCall(t *testing.T, ledger *recordingQuotaLedger, job generation.Job) {
	t.Helper()
	if ledger.releaseCalls != 1 {
		t.Fatalf("releaseCalls = %d, want 1", ledger.releaseCalls)
	}
	if ledger.tenantID != job.TenantID || ledger.jobID != job.ID || ledger.period != job.BudgetDate || ledger.amount != job.BudgetMicroUSD {
		t.Fatalf("release call = (%q, %q, %q, %d), want (%q, %q, %q, %d)", ledger.tenantID, ledger.jobID, ledger.period, ledger.amount, job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
	}
}

func assertReleaseOpsAppended(t *testing.T, ops []kv.WriteOp) {
	t.Helper()
	if ops[len(ops)-2].Update == nil || ops[len(ops)-2].Update.Key.PK != "AGGREGATE_RELEASE" {
		t.Fatalf("penultimate op = %#v, want aggregate release update", ops[len(ops)-2])
	}
	if ops[len(ops)-1].Update == nil || ops[len(ops)-1].Update.Key.PK != "LEDGER_RELEASE" {
		t.Fatalf("last op = %#v, want ledger release update", ops[len(ops)-1])
	}
}
