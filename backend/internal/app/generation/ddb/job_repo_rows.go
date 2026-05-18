package ddb

import (
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func buildStageAttemptItem(job *generation.Job, r genapp.StageResult, now time.Time, traceparent string) map[string]any {
	attemptNo := job.Attempts + 1
	result := "SUCCESS"
	errorCode := ""
	errorMessage := ""
	if r.TerminalError != nil {
		result = "TERMINAL_FAILURE"
		errorCode = r.TerminalError.Code
		errorMessage = r.TerminalError.Message
	} else if r.AttemptsDelta > 0 && r.NextStage == job.CurrentStage {
		result = "TRANSIENT_FAILURE"
		if r.TransientError != nil {
			errorCode = r.TransientError.Code
			errorMessage = r.TransientError.Message
		}
	}
	row := map[string]any{
		"PK":             JobPK(job.ID),
		"SK":             StageAttemptSK(string(job.CurrentStage), job.StageVersion, attemptNo),
		"item_type":      "STAGE_ATTEMPT",
		"tenant_id":      job.TenantID,
		"job_id":         job.ID,
		"stage":          string(job.CurrentStage),
		"stage_version":  job.StageVersion,
		"attempt_no":     attemptNo,
		"result":         result,
		"next_stage":     string(r.NextStage),
		"resource_class": string(r.ResourceClass),
		"error_code":     errorCode,
		"error_message":  errorMessage,
		"created_at":     now.Format(time.RFC3339Nano),
	}
	if traceparent != "" {
		row["traceparent"] = traceparent
		if traceID := genapp.TraceIDFromTraceparent(traceparent); traceID != "" {
			row["trace_id"] = traceID
		}
	}
	return row
}

func buildTerminalItem(job *generation.Job, r genapp.StageResult, now time.Time) map[string]any {
	status := generation.StatusComplete
	errorCode := ""
	errorMessage := ""
	completedAt := now
	if r.CompletedAt != nil {
		completedAt = r.CompletedAt.UTC()
	}
	if r.TerminalError != nil {
		status = generation.StatusFailed
		errorCode = r.TerminalError.Code
		errorMessage = r.TerminalError.Message
	}
	return map[string]any{
		"PK":            JobPK(job.ID),
		"SK":            TerminalSK,
		"item_type":     "TERMINAL",
		"tenant_id":     job.TenantID,
		"job_id":        job.ID,
		"status":        string(status),
		"error_code":    errorCode,
		"error_message": errorMessage,
		"created_at":    completedAt.Format(time.RFC3339Nano),
		"ttl_epoch":     completedAt.Add(365 * 24 * time.Hour).Unix(),
	}
}

func buildCancelledTerminalItem(job generation.Job, reason string, now time.Time) map[string]any {
	return map[string]any{
		"PK":            JobPK(job.ID),
		"SK":            TerminalSK,
		"item_type":     "TERMINAL",
		"tenant_id":     job.TenantID,
		"job_id":        job.ID,
		"status":        string(generation.StatusCancelled),
		"error_code":    "CANCELLED",
		"error_message": reason,
		"created_at":    now.Format(time.RFC3339Nano),
		"ttl_epoch":     now.Add(365 * 24 * time.Hour).Unix(),
	}
}

// buildAuditItem materializes the per-job AUDIT#GATE row from a populated
// GateDecision. Caller (AdvanceStageAndEnqueue) only invokes this when the
// gate actually ran — non-gate terminal failures (RETRY_EXHAUSTED,
// BUDGET_EXHAUSTED, …) intentionally have no gate audit row so the
// AUDIT#GATE partition doesn't accumulate placeholders that mislead the
// operator console into reading "gate failed" when the gate never ran.
func buildAuditItem(job *generation.Job, d genapp.GateDecision, now time.Time) map[string]any {
	if d.JobID == "" {
		d.JobID = job.ID
	}
	if d.TenantID == "" {
		d.TenantID = job.TenantID
	}
	if d.OutputType == "" {
		d.OutputType = string(job.OutputType)
	}
	if d.Model == "" {
		d.Model = job.Model
	}
	if d.GateVersion == "" {
		d.GateVersion = "v1"
	}
	return BuildGateAuditRow(d, now)
}
