package generation

import (
	"context"
	"testing"
	"time"

	domain "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestResolveTransition_CurrentGraph(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	ledger := &transitionLedger{}
	w := &Workflow{
		Clock:       func() time.Time { return now },
		QuotaLedger: ledger,
	}
	ctx := context.Background()

	cases := []struct {
		name      string
		stage     domain.Stage
		outcome   StageOutcome
		wantStage domain.Stage
		wantClass domain.ResourceClass
	}{
		{
			name:      "input moderation pass",
			stage:     domain.StageInputModeration,
			outcome:   OutcomeModerationPassed,
			wantStage: domain.StageCostReserve,
			wantClass: domain.ResourceFast,
		},
		{
			name:      "budget reserved",
			stage:     domain.StageCostReserve,
			outcome:   OutcomeBudgetReserved,
			wantStage: domain.StagePromptPrepare,
			wantClass: domain.ResourceFast,
		},
		{
			name:      "prompt prepared",
			stage:     domain.StagePromptPrepare,
			outcome:   OutcomePromptPrepared,
			wantStage: domain.StageProviderSubmit,
			wantClass: domain.ResourceProvider,
		},
		{
			name:      "async provider submitted",
			stage:     domain.StageProviderSubmit,
			outcome:   OutcomeProviderSubmittedAsync,
			wantStage: domain.StageProviderWait,
			wantClass: domain.ResourcePoll,
		},
		{
			name:      "sync artifact staged",
			stage:     domain.StageProviderSubmit,
			outcome:   OutcomeArtifactStaged,
			wantStage: domain.StageOutputModeration,
			wantClass: domain.ResourceFast,
		},
		{
			name:      "async artifact staged",
			stage:     domain.StageProviderWait,
			outcome:   OutcomeArtifactStaged,
			wantStage: domain.StageOutputModeration,
			wantClass: domain.ResourceFast,
		},
		{
			name:      "poll pending",
			stage:     domain.StageProviderWait,
			outcome:   OutcomePollPending,
			wantStage: domain.StageProviderWait,
			wantClass: domain.ResourcePoll,
		},
		{
			name:      "output moderation pass",
			stage:     domain.StageOutputModeration,
			outcome:   OutcomeModerationPassed,
			wantStage: domain.StageDisclosurePostprocess,
			wantClass: domain.ResourceImageProcess,
		},
		{
			name:      "disclosure complete",
			stage:     domain.StageDisclosurePostprocess,
			outcome:   OutcomeDisclosureComplete,
			wantStage: domain.StagePublish,
			wantClass: domain.ResourceFast,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := transitionJob(tc.stage)
			result := StageResult{
				Outcome:        tc.outcome,
				BudgetDate:     "20260518",
				BudgetMicroUSD: 42,
			}
			resolved, err := w.resolveTransition(ctx, job, result)
			if err != nil {
				t.Fatalf("resolveTransition: %v", err)
			}
			if resolved.NextStage != tc.wantStage {
				t.Fatalf("NextStage = %s, want %s", resolved.NextStage, tc.wantStage)
			}
			if resolved.ResourceClass != tc.wantClass {
				t.Fatalf("ResourceClass = %s, want %s", resolved.ResourceClass, tc.wantClass)
			}
			msg, err := UnmarshalStageMessage(resolved.OutboxBody)
			if err != nil {
				t.Fatalf("UnmarshalStageMessage: %v", err)
			}
			if msg.Stage != tc.wantStage {
				t.Fatalf("message stage = %s, want %s", msg.Stage, tc.wantStage)
			}
			if msg.StageVersion != job.StageVersion+1 {
				t.Fatalf("message version = %d, want %d", msg.StageVersion, job.StageVersion+1)
			}
			if msg.ResourceClass != tc.wantClass {
				t.Fatalf("message resource class = %s, want %s", msg.ResourceClass, tc.wantClass)
			}
		})
	}
}

func TestResolveTransition_ReplayEquivalentMutationsArePreserved(t *testing.T) {
	w := &Workflow{}
	job := transitionJob(domain.StageProviderSubmit)
	result := StageResult{
		Outcome:       OutcomeProviderSubmittedAsync,
		ProviderJobID: "provider-job-replay",
	}
	resolved, err := w.resolveTransition(context.Background(), job, result)
	if err != nil {
		t.Fatalf("resolveTransition: %v", err)
	}
	if resolved.ProviderJobID != result.ProviderJobID {
		t.Fatalf("ProviderJobID = %q, want %q", resolved.ProviderJobID, result.ProviderJobID)
	}
	if resolved.NextStage != domain.StageProviderWait {
		t.Fatalf("NextStage = %s, want PROVIDER_WAIT", resolved.NextStage)
	}

	job = transitionJob(domain.StageDisclosurePostprocess)
	result = StageResult{
		Outcome:       OutcomeDisclosureComplete,
		ResultAssetID: "asset-replay",
	}
	resolved, err = w.resolveTransition(context.Background(), job, result)
	if err != nil {
		t.Fatalf("resolveTransition: %v", err)
	}
	if resolved.ResultAssetID != result.ResultAssetID {
		t.Fatalf("ResultAssetID = %q, want %q", resolved.ResultAssetID, result.ResultAssetID)
	}
	if resolved.NextStage != domain.StagePublish {
		t.Fatalf("NextStage = %s, want PUBLISH", resolved.NextStage)
	}
}

func TestResolveTransition_LedgerOps(t *testing.T) {
	ledger := &transitionLedger{}
	w := &Workflow{QuotaLedger: ledger}
	ctx := context.Background()

	reserved, err := w.resolveTransition(ctx, transitionJob(domain.StageCostReserve), StageResult{
		Outcome:        OutcomeBudgetReserved,
		BudgetDate:     "20260518",
		BudgetMicroUSD: 42,
	})
	if err != nil {
		t.Fatalf("reserve transition: %v", err)
	}
	assertLedgerItems(t, reserved.LedgerOp, "reserve")
	if ledger.reserveAmount != 42 || ledger.reservePeriod != "20260518" || ledger.reserveAttempt != 3 {
		t.Fatalf("reserve ledger = amount %d period %q attempt %d", ledger.reserveAmount, ledger.reservePeriod, ledger.reserveAttempt)
	}

	job := transitionJob(domain.StageProviderSubmit)
	job.BudgetDate = "20260518"
	job.BudgetMicroUSD = 42
	committed, err := w.resolveTransition(ctx, job, StageResult{Outcome: OutcomeArtifactStaged})
	if err != nil {
		t.Fatalf("commit transition: %v", err)
	}
	assertLedgerItems(t, committed.LedgerOp, "commit")
	if ledger.commitAmount != 42 || ledger.commitPeriod != "20260518" {
		t.Fatalf("commit ledger = amount %d period %q", ledger.commitAmount, ledger.commitPeriod)
	}
}

func TestResolveTransition_TransientRetrySelfTransition(t *testing.T) {
	w := &Workflow{}
	job := transitionJob(domain.StageProviderSubmit)
	result := StageResult{
		Outcome:       OutcomeTransientRetry,
		AttemptsDelta: 1,
		TransientError: &domain.Error{
			Code:    "PROVIDER_TIMEOUT",
			Message: "timeout",
		},
	}
	resolved, err := w.resolveTransition(context.Background(), job, result)
	if err != nil {
		t.Fatalf("resolveTransition: %v", err)
	}
	if resolved.NextStage != domain.StageProviderSubmit {
		t.Fatalf("NextStage = %s, want PROVIDER_SUBMIT", resolved.NextStage)
	}
	if resolved.ResourceClass != domain.ResourceProvider {
		t.Fatalf("ResourceClass = %s, want PROVIDER", resolved.ResourceClass)
	}
	if resolved.AttemptsDelta != 1 {
		t.Fatalf("AttemptsDelta = %d, want 1", resolved.AttemptsDelta)
	}
	msg, err := UnmarshalStageMessage(resolved.OutboxBody)
	if err != nil {
		t.Fatalf("UnmarshalStageMessage: %v", err)
	}
	if msg.Stage != job.CurrentStage || msg.StageVersion != job.StageVersion+1 {
		t.Fatalf("message = stage %s version %d, want %s %d", msg.Stage, msg.StageVersion, job.CurrentStage, job.StageVersion+1)
	}
}

func TestResolveTransition_ProviderJobFailedReleasesBudget(t *testing.T) {
	ledger := &transitionLedger{}
	w := &Workflow{QuotaLedger: ledger}
	job := transitionJob(domain.StageProviderWait)
	job.BudgetDate = "20260518"
	job.BudgetMicroUSD = 42

	resolved, err := w.resolveTransition(context.Background(), job, StageResult{Outcome: OutcomeProviderJobFailed})
	if err != nil {
		t.Fatalf("resolveTransition: %v", err)
	}
	if resolved.NextStage != StageTerminal {
		t.Fatalf("NextStage = %s, want TERMINAL", resolved.NextStage)
	}
	if resolved.TerminalError == nil || resolved.TerminalError.Code != "PROVIDER_JOB_FAILED" {
		t.Fatalf("TerminalError = %+v, want PROVIDER_JOB_FAILED", resolved.TerminalError)
	}
	assertLedgerItems(t, resolved.LedgerOp, "release")
	if ledger.releaseAmount != 42 || ledger.releasePeriod != "20260518" {
		t.Fatalf("release ledger = amount %d period %q", ledger.releaseAmount, ledger.releasePeriod)
	}
}

func TestResolveTransition_PublishedCompletesTerminal(t *testing.T) {
	now := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	w := &Workflow{Clock: func() time.Time { return now }}
	resolved, err := w.resolveTransition(context.Background(), transitionJob(domain.StagePublish), StageResult{Outcome: OutcomePublished})
	if err != nil {
		t.Fatalf("resolveTransition: %v", err)
	}
	if resolved.NextStage != StageTerminal {
		t.Fatalf("NextStage = %s, want TERMINAL", resolved.NextStage)
	}
	if resolved.CompletedAt == nil || !resolved.CompletedAt.Equal(now) {
		t.Fatalf("CompletedAt = %v, want %v", resolved.CompletedAt, now)
	}
}

func TestResolveTransition_ErrorsOnMixedOrUnknownRouting(t *testing.T) {
	w := &Workflow{}
	_, err := w.resolveTransition(context.Background(), transitionJob(domain.StageInputModeration), StageResult{
		Outcome:   OutcomeModerationPassed,
		NextStage: domain.StageCostReserve,
	})
	if err == nil || domain.AsError(err).Code != "MIXED_STAGE_ROUTING" {
		t.Fatalf("mixed routing err = %v, want MIXED_STAGE_ROUTING", err)
	}

	_, err = w.resolveTransition(context.Background(), transitionJob(domain.StagePromptPrepare), StageResult{
		Outcome: OutcomeModerationPassed,
	})
	if err == nil || domain.AsError(err).Code != "UNKNOWN_STAGE_TRANSITION" {
		t.Fatalf("unknown transition err = %v, want UNKNOWN_STAGE_TRANSITION", err)
	}
}

func transitionJob(stage domain.Stage) *domain.Job {
	return &domain.Job{
		ID:             "gen-transition",
		TenantID:       "tenant-transition",
		CurrentStage:   stage,
		StageVersion:   2,
		Attempts:       2,
		OutputType:     domain.OutputImage,
		Tier:           domain.TierPaid,
		BudgetDate:     "20260518",
		BudgetMicroUSD: 42,
	}
}

type transitionLedger struct {
	reservePeriod  string
	reserveAmount  int64
	reserveAttempt int
	commitPeriod   string
	commitAmount   int64
	releasePeriod  string
	releaseAmount  int64
}

func (l *transitionLedger) LedgerPutReserved(_ string, _ string, period string, amount int64, attempt int) LedgerOp {
	l.reservePeriod = period
	l.reserveAmount = amount
	l.reserveAttempt = attempt
	return transitionLedgerOp()
}

func (l *transitionLedger) LedgerUpdateCommitted(_ string, _ string, period string, amount int64) LedgerOp {
	l.commitPeriod = period
	l.commitAmount = amount
	return transitionLedgerOp()
}

func (l *transitionLedger) LedgerUpdateReleased(_ string, _ string, period string, amount int64) LedgerOp {
	l.releasePeriod = period
	l.releaseAmount = amount
	return transitionLedgerOp()
}

func transitionLedgerOp() LedgerOp {
	return LedgerOp{Items: []kv.WriteOp{
		{Update: &kv.UpdateOp{}},
		{Put: &kv.PutOp{}},
	}}
}

func assertLedgerItems(t *testing.T, op *LedgerOp, name string) {
	t.Helper()
	if op == nil {
		t.Fatalf("%s ledger op is nil", name)
	}
	if len(op.Items) != 2 {
		t.Fatalf("%s ledger item count = %d, want 2", name, len(op.Items))
	}
	if op.Items[0].Update == nil {
		t.Fatalf("%s ledger item 0 is not aggregate update", name)
	}
	if op.Items[1].Put == nil {
		t.Fatalf("%s ledger item 1 is not ledger put/update", name)
	}
}
