package admin

import (
	"context"
	"errors"
	"fmt"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) SuspendTenant(ctx context.Context, req *connect.Request[adminpb.SuspendTenantRequest]) (*connect.Response[adminpb.SuspendTenantResponse], error) {
	claims, err := authz.RequireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetTenantId() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("tenant_id and reason required"))
	}
	tenantID, err := tenantIDForAdminRequest(claims, req.Msg.GetTenantId(), true)
	if err != nil {
		return nil, err
	}
	if err := s.tenant.Suspend(ctx, tenantID, req.Msg.GetReason(), claims.Subject); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("suspend tenant: %w", err))
	}
	return connect.NewResponse(&adminpb.SuspendTenantResponse{}), nil
}

func (s *Server) UnsuspendTenant(ctx context.Context, req *connect.Request[adminpb.UnsuspendTenantRequest]) (*connect.Response[adminpb.UnsuspendTenantResponse], error) {
	claims, err := authz.RequireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetTenantId() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("tenant_id and reason required"))
	}
	tenantID, err := tenantIDForAdminRequest(claims, req.Msg.GetTenantId(), true)
	if err != nil {
		return nil, err
	}
	if err := s.tenant.Unsuspend(ctx, tenantID, req.Msg.GetReason(), claims.Subject); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unsuspend tenant: %w", err))
	}
	return connect.NewResponse(&adminpb.UnsuspendTenantResponse{}), nil
}
