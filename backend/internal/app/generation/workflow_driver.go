package generation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

const retryExhaustedCode = "RETRY_EXHAUSTED"

// Run drives the FSM to a terminal status. It is idempotent at the stage
// level via the IdempotencyStore.
func (w *Workflow) Run(ctx context.Context, jobID string) error {
	// First lookup uses empty tenant; mem repo allows it. Production callers
	// dispatching from SQS pass the tenant id in the stage message and use
	// the worker-side RunStage entry point instead.
	job, err := w.Repo.GetJob(ctx, "", jobID)
	if err != nil {
		return fmt.Errorf("workflow: get job: %w", err)
	}
	if job.CurrentStage == "" {
		job.CurrentStage = generation.StageInputModeration
	}
	outputType := string(job.OutputType)
	for {
		result, runErr := w.RunStage(ctx, job)
		if runErr != nil {
			classified := generation.AsError(runErr)
			if classified.Terminal {
				if err := w.Repo.AdvanceStageAndEnqueue(ctx, job, w.terminalFailResultFromError(ctx, job, runErr)); err != nil {
					return fmt.Errorf("workflow: persist terminal %s: %w", classified.Code, err)
				}
				w.emitTerminal(ctx, generation.StatusFailed, classified.Code, outputType)
				return classified
			}
			if w.retryExhausted(job) {
				exhausted := retryExhaustedError(runErr)
				if err := w.Repo.AdvanceStageAndEnqueue(ctx, job, w.retryExhaustedResult(ctx, job, exhausted)); err != nil {
					return fmt.Errorf("workflow: persist retry exhausted: %w", err)
				}
				w.emitTerminal(ctx, generation.StatusFailed, exhausted.Code, outputType)
				return exhausted
			}
			result = w.transientRetryResult(ctx, job, classified)
			if err := w.Repo.AdvanceStageAndEnqueue(ctx, job, result); err != nil {
				return fmt.Errorf("workflow: persist transient retry: %w", err)
			}
			applyMutations(job, result)
			return runErr
		}
		if err := w.Repo.AdvanceStageAndEnqueue(ctx, job, result); err != nil {
			if handled, handleErr := w.handleAdvanceTerminalError(ctx, job, err, outputType); handled {
				if handleErr != nil {
					return handleErr
				}
				return generation.AsError(err)
			}
			return fmt.Errorf("workflow: advance stage: %w", err)
		}
		applyMutations(job, result)
		if result.IsTerminalComplete() {
			w.emitTerminal(ctx, generation.StatusComplete, "", outputType)
			return nil
		}
		if result.IsTerminalFailed() {
			code := ""
			if result.TerminalError != nil {
				code = result.TerminalError.Code
			}
			w.emitTerminal(ctx, generation.StatusFailed, code, outputType)
			return nil
		}
	}
}

// AdvanceOneStage runs the currently persisted stage once and commits the
// resulting state transition. Per-message worker entry: the originating SQS
// message is acked on every committed outcome — success, terminal, retry-
// exhausted, and transient retry — because retries ride the outbox-enqueued
// stage_version+1 message, not SQS redelivery. Only an uncommitted infra
// failure (DDB write, etc.) leaves the message in-flight so SQS can redeliver
// the same stage_version.
func (w *Workflow) AdvanceOneStage(ctx context.Context, job *generation.Job) error {
	outputType := string(job.OutputType)
	result, runErr := w.RunStage(ctx, job)
	if runErr != nil {
		classified := generation.AsError(runErr)
		if classified.Terminal {
			err := w.Repo.AdvanceStageAndEnqueue(ctx, job, w.terminalFailResultFromError(ctx, job, runErr))
			w.emitTerminal(ctx, generation.StatusFailed, classified.Code, outputType)
			return err
		}
		if w.retryExhausted(job) {
			exhausted := retryExhaustedError(runErr)
			err := w.Repo.AdvanceStageAndEnqueue(ctx, job, w.retryExhaustedResult(ctx, job, exhausted))
			w.emitTerminal(ctx, generation.StatusFailed, exhausted.Code, outputType)
			return err
		}
		result = w.transientRetryResult(ctx, job, classified)
		if err := w.Repo.AdvanceStageAndEnqueue(ctx, job, result); err != nil {
			return err
		}
		slog.InfoContext(ctx, "generation stage transient retry enqueued",
			"job_id", job.ID,
			"stage", string(job.CurrentStage),
			"stage_version", job.StageVersion+1,
			"attempt", job.Attempts+1,
			"error_code", classified.Code,
		)
		return nil
	}
	if err := w.Repo.AdvanceStageAndEnqueue(ctx, job, result); err != nil {
		if handled, handleErr := w.handleAdvanceTerminalError(ctx, job, err, outputType); handled {
			return handleErr
		}
		return err
	}
	switch result.NextStage {
	case StageTerminal:
		if result.IsTerminalFailed() {
			code := ""
			if result.TerminalError != nil {
				code = result.TerminalError.Code
			}
			w.emitTerminal(ctx, generation.StatusFailed, code, outputType)
			return nil
		}
		w.emitTerminal(ctx, generation.StatusComplete, "", outputType)
	}
	return nil
}

// terminalFailResult builds a terminal-failed StageResult, attaching a
// budget-ledger release when the current stage held a reservation. Shared
// between Run (driver loop) and AdvanceOneStage (per-message worker entry).
func (w *Workflow) terminalFailResult(ctx context.Context, job *generation.Job, err *generation.Error) StageResult {
	r := StageResult{NextStage: StageTerminal, TerminalError: err}
	if BudgetReleaseAllowed(job.CurrentStage) {
		w.attachLedgerRelease(ctx, job, &r)
	}
	return r
}

func (w *Workflow) terminalFailResultFromError(ctx context.Context, job *generation.Job, runErr error) StageResult {
	classified := generation.AsError(runErr)
	r := w.terminalFailResult(ctx, job, classified)
	if decision, ok := gateDecisionFromError(runErr); ok {
		r.GateDecision = &decision
	}
	return r
}

func (w *Workflow) transientRetryResult(ctx context.Context, job *generation.Job, err *generation.Error) StageResult {
	class := stageWorkClass(job)
	body, _ := MarshalStageMessage(job.TenantID, job.ID, job.CurrentStage, job.StageVersion+1, class, TraceparentFromContext(ctx))
	return StageResult{
		NextStage:      job.CurrentStage,
		OutboxBody:     body,
		ResourceClass:  class,
		AttemptsDelta:  1,
		TransientError: err,
	}
}

func (w *Workflow) retryExhaustedResult(ctx context.Context, job *generation.Job, err *generation.Error) StageResult {
	result := w.terminalFailResult(ctx, job, err)
	result.ResourceClass = stageWorkClass(job)
	result.AttemptsDelta = 1
	return result
}

func (w *Workflow) retryExhausted(job *generation.Job) bool {
	return w.MaxRetries > 0 && job.Attempts+1 >= w.MaxRetries
}

func retryExhaustedError(err error) *generation.Error {
	classified := generation.AsError(err)
	message := err.Error()
	if classified != nil && classified.Code != "" {
		message = classified.Code + ": " + classified.Message
	}
	return &generation.Error{Code: retryExhaustedCode, Message: message, Terminal: true}
}

func (w *Workflow) handleAdvanceTerminalError(ctx context.Context, job *generation.Job, err error, outputType string) (bool, error) {
	classified := generation.AsError(err)
	if classified == nil || !classified.Terminal || classified.Code != "BUDGET_EXHAUSTED" {
		return false, nil
	}
	if commitErr := w.Repo.AdvanceStageAndEnqueue(ctx, job, w.terminalFailResult(ctx, job, classified)); commitErr != nil {
		return true, fmt.Errorf("workflow: persist terminal %s after advance failure: %w", classified.Code, commitErr)
	}
	w.emitTerminal(ctx, generation.StatusFailed, classified.Code, outputType)
	return true, nil
}

func applyMutations(job *generation.Job, r StageResult) {
	job.CurrentStage = r.NextStage
	job.StageVersion++
	if r.AttemptsDelta != 0 {
		job.Attempts += r.AttemptsDelta
	}
	if r.BudgetDate != "" {
		job.BudgetDate = r.BudgetDate
	}
	if r.BudgetMicroUSD != 0 {
		job.BudgetMicroUSD = r.BudgetMicroUSD
	}
	if r.PreparedPrompt != "" {
		job.PreparedPrompt = r.PreparedPrompt
	}
	if r.PreparedPromptHash != "" {
		job.PreparedPromptHash = r.PreparedPromptHash
	}
	if r.PromptSpecVersion != "" {
		job.PromptSpecVersion = r.PromptSpecVersion
	}
	if r.GenerationParamsHash != "" {
		job.GenerationParamsHash = r.GenerationParamsHash
	}
	if r.ProviderRequestID != "" {
		job.ProviderRequestID = r.ProviderRequestID
	}
	if r.ResultAssetID != "" {
		job.ResultAssetID = r.ResultAssetID
	}
	if r.ProviderJobID != "" {
		job.ProviderJobID = r.ProviderJobID
	}
	if r.CompletedAt != nil {
		job.CompletedAt = r.CompletedAt
		job.Status = generation.StatusComplete
	}
	if r.TerminalError != nil {
		job.Error = r.TerminalError
		job.Status = generation.StatusFailed
	}
}
