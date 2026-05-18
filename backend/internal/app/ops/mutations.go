package ops

import (
	"context"
	"fmt"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	gendb "github.com/dtszwai/media-processing-service/backend/internal/app/generation/ddb"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// CancelJob delegates to JobRepo.CancelJob which holds the canonical cancel
// transition (terminal media-lifecycle flip + cancelled terminal row +
// generation/output rollups). The audit row sits alongside the FSM's own
// terminal write.
func (s *Service) CancelJob(ctx context.Context, jobID, reason string) error {
	if jobID == "" {
		return fmt.Errorf("ops: job_id required")
	}
	if s.JobRepo == nil {
		return fmt.Errorf("ops: job repo not wired")
	}
	job, err := s.JobRepo.GetJob(ctx, "", jobID)
	if err != nil {
		return fmt.Errorf("ops: get job: %w", err)
	}
	if reason == "" {
		reason = "OPERATOR_CANCELLED"
	}
	if err := s.JobRepo.CancelJob(ctx, job.TenantID, jobID, reason); err != nil {
		return fmt.Errorf("ops: cancel job: %w", err)
	}
	s.audit(ctx, AuditEvent{
		Operation: "CancelJob",
		Target:    jobID,
		Details:   map[string]string{"reason": reason},
	})
	return nil
}

// RetryJob writes a fresh outbox row for the current_stage so the next
// SQS pump runs the FSM again. The job's status flips back to RUNNING; the
// terminal row, if present, stays as the historical record.
func (s *Service) RetryJob(ctx context.Context, jobID string) error {
	job, err := s.lookupJob(ctx, jobID)
	if err != nil {
		return err
	}
	now := s.now()
	body, _ := genapp.MarshalStageMessage(job.TenantID, job.ID, job.CurrentStage, job.StageVersion, generation.ResourceFast, "")
	outboxItem := outbox.JobItem(outbox.JobRow{
		JobID:         job.ID,
		TenantID:      job.TenantID,
		TenantLane:    genapp.TenantLane(job.TenantID),
		Tier:          string(job.Tier),
		Stage:         string(job.CurrentStage),
		ResourceClass: string(generation.ResourceFast),
		Body:          body,
		PartitionTS:   now,
	})
	ops := []kv.WriteOp{
		{Update: &kv.UpdateOp{
			Key:                      kv.Key{PK: gendb.JobPK(job.ID), SK: gendb.JobSK},
			UpdateExpression:         "SET #st = :running, updated_at = :now, gsi_job_pk = :gpk",
			ExpressionAttributeNames: kv.Names{"#st": "status"},
			ExpressionAttributeValues: kv.Values{
				":running": string(generation.StatusRunning),
				":now":     now.Format(time.RFC3339Nano),
				":gpk":     "TENANT#" + job.TenantID + "#STATUS#" + string(generation.StatusRunning),
			},
		}},
		{Put: &kv.PutOp{Item: outboxItem, ConditionExpression: "attribute_not_exists(PK)"}},
	}
	if err := s.KV.TransactWrite(ctx, ops); err != nil {
		return fmt.Errorf("ops: retry job: %w", err)
	}
	s.audit(ctx, AuditEvent{
		Operation: "RetryJob",
		Target:    jobID,
		Details:   map[string]string{"stage": string(job.CurrentStage)},
	})
	return nil
}

// ForceFailJob terminally fails the job. Useful when a stuck job needs to
// be cleared from the active list without a clean cancel path (e.g. the
// FSM state is corrupt). The repo owns the canonical terminal transition:
// job row, media lifecycle, terminal row, and generation/output rollups.
func (s *Service) ForceFailJob(ctx context.Context, jobID, errorCode, errorMessage string) error {
	job, err := s.lookupJob(ctx, jobID)
	if err != nil {
		return err
	}
	if errorCode == "" {
		errorCode = "OPERATOR_FORCED_FAIL"
	}
	if err := s.JobRepo.ForceFailJob(ctx, job.TenantID, jobID, errorCode, errorMessage); err != nil {
		return fmt.Errorf("ops: force-fail: %w", err)
	}
	s.audit(ctx, AuditEvent{
		Operation: "ForceFailJob",
		Target:    jobID,
		Details:   map[string]string{"error_code": errorCode, "error_message": errorMessage},
	})
	return nil
}

// ReplayOutbox writes a fresh outbox row for the job's current_stage with a
// PartitionTS=now so the relay picks it up immediately. Useful when the
// previous row was orphaned (e.g. relay crashed).
func (s *Service) ReplayOutbox(ctx context.Context, jobID string) error {
	job, err := s.lookupJob(ctx, jobID)
	if err != nil {
		return err
	}
	now := s.now()
	body, _ := genapp.MarshalStageMessage(job.TenantID, job.ID, job.CurrentStage, job.StageVersion, generation.ResourceFast, "")
	outboxItem := outbox.JobItem(outbox.JobRow{
		JobID:         job.ID,
		TenantID:      job.TenantID,
		TenantLane:    genapp.TenantLane(job.TenantID),
		Tier:          string(job.Tier),
		Stage:         string(job.CurrentStage),
		ResourceClass: string(generation.ResourceFast),
		Body:          body,
		PartitionTS:   now,
	})
	if err := s.KV.Put(ctx, outboxItem, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK)",
	}); err != nil {
		return fmt.Errorf("ops: replay outbox: %w", err)
	}
	s.audit(ctx, AuditEvent{
		Operation: "ReplayOutbox",
		Target:    jobID,
	})
	return nil
}

func (s *Service) lookupJob(ctx context.Context, jobID string) (*generation.Job, error) {
	if jobID == "" {
		return nil, fmt.Errorf("ops: job_id required")
	}
	if s.JobRepo == nil {
		return nil, fmt.Errorf("ops: job repo not wired")
	}
	job, err := s.JobRepo.GetJob(ctx, "", jobID)
	if err != nil {
		return nil, fmt.Errorf("ops: lookup job: %w", err)
	}
	return job, nil
}
