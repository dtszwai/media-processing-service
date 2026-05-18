package admin

import (
	"context"
	"errors"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
)

type WorkflowCanceller interface {
	CancelJob(ctx context.Context, tenantID, jobID, reason string) error
}

type WorkflowAdmin struct {
	Canceller WorkflowCanceller
	Recorder  auditapp.Recorder
}

func NewWorkflowAdmin(canceller WorkflowCanceller, recorder auditapp.Recorder) *WorkflowAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &WorkflowAdmin{Canceller: canceller, Recorder: recorder}
}

func (a *WorkflowAdmin) Cancel(ctx context.Context, tenantID, jobID, reason, actorUserID string) error {
	if a == nil || a.Canceller == nil {
		return errors.New("workflow admin: canceller required")
	}
	if tenantID == "" || jobID == "" {
		return errors.New("workflow admin: tenant_id and job_id required")
	}
	if reason == "" {
		return errors.New("workflow admin: reason required")
	}
	if err := a.Canceller.CancelJob(ctx, tenantID, jobID, reason); err != nil {
		return err
	}
	return a.Recorder.Record(ctx, auditapp.NewWorkflowJobCancelled(audit.ActorOperator, actorUserID, tenantID, jobID, reason, ""))
}
