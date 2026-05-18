package generation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// DLQConsumer converts generation stage messages that reached a queue DLQ into
// ordinary terminal workflow transitions. It shares the same repository path as
// the stage runner so budget release, terminal rows, output failures, and audit
// rows remain one transaction.
type DLQConsumer struct {
	Repo     JobRepository
	Attempts StageAttemptReader
	Ledger   QuotaLedger
}

func (c *DLQConsumer) ProcessMessage(ctx context.Context, body []byte) error {
	msg, err := UnmarshalStageMessage(body)
	if err != nil {
		return fmt.Errorf("generation dlq: decode stage message: %w", err)
	}
	if msg.TenantID == "" || msg.JobID == "" || msg.Stage == "" || msg.StageVersion == 0 {
		return fmt.Errorf("generation dlq: stage message missing tenant_id/job_id/stage/stage_version")
	}
	job, err := c.Repo.GetJob(ctx, msg.TenantID, msg.JobID)
	if err != nil {
		return fmt.Errorf("generation dlq: get job: %w", err)
	}
	if isTerminalJob(job) {
		return nil
	}
	if job.CurrentStage != msg.Stage || job.StageVersion != msg.StageVersion {
		slog.InfoContext(ctx, "dropping stale generation dlq stage message",
			"job_id", msg.JobID,
			"message_stage", msg.Stage,
			"current_stage", job.CurrentStage,
			"message_version", msg.StageVersion,
			"current_version", job.StageVersion,
		)
		return nil
	}
	result := StageResult{
		NextStage:     StageTerminal,
		ResourceClass: msg.ResourceClass,
		TerminalError: retryExhaustedDLQError(c.lastAttempt(ctx, msg)),
	}
	if result.ResourceClass == "" {
		result.ResourceClass = stageWorkClass(job)
	}
	if c.Ledger != nil && job.BudgetDate != "" && BudgetReleaseAllowed(job.CurrentStage) {
		op := c.Ledger.LedgerUpdateReleased(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
		result.LedgerOp = &op
	}
	return c.Repo.AdvanceStageAndEnqueue(ctx, job, result)
}

func (c *DLQConsumer) lastAttempt(ctx context.Context, msg StageMessage) StageAttempt {
	if c.Attempts == nil {
		return StageAttempt{}
	}
	attempt, err := c.Attempts.LastStageAttempt(ctx, msg.TenantID, msg.JobID, msg.Stage)
	if err != nil {
		return StageAttempt{}
	}
	return attempt
}

func retryExhaustedDLQError(attempt StageAttempt) *generation.Error {
	message := "generation stage message reached DLQ after retry exhaustion"
	if attempt.ErrorCode != "" {
		message = fmt.Sprintf("%s; last attempt error_code=%s", message, attempt.ErrorCode)
	}
	return &generation.Error{
		Code:     retryExhaustedCode,
		Message:  message,
		Terminal: true,
	}
}

func isTerminalJob(job *generation.Job) bool {
	switch job.Status {
	case generation.StatusComplete, generation.StatusFailed, generation.StatusCancelled:
		return true
	default:
		return job.CurrentStage == generation.StageTerminal
	}
}
