package admin

import (
	"context"
	"errors"
	"fmt"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) CancelWorkflowJob(ctx context.Context, req *connect.Request[adminpb.CancelWorkflowJobRequest]) (*connect.Response[adminpb.CancelWorkflowJobResponse], error) {
	claims, err := authz.RequireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetJobId() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("job_id and reason required"))
	}
	if err := s.workflow.Cancel(ctx, claims.TenantID, req.Msg.GetJobId(), req.Msg.GetReason(), claims.Subject); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cancel workflow job: %w", err))
	}
	return connect.NewResponse(&adminpb.CancelWorkflowJobResponse{}), nil
}
