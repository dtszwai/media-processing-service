package admin

import (
	"context"
	"errors"
	"fmt"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) OverrideQuotaCap(ctx context.Context, req *connect.Request[adminpb.OverrideQuotaCapRequest]) (*connect.Response[adminpb.OverrideQuotaCapResponse], error) {
	claims, err := authz.RequireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetTenantId() == "" || req.Msg.GetMetric() == "" || req.Msg.GetPeriod() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("tenant_id, metric, period, and reason required"))
	}
	tenantID, err := tenantIDForAdminRequest(claims, req.Msg.GetTenantId(), true)
	if err != nil {
		return nil, err
	}
	result, err := s.quota.OverrideTenantCap(ctx, tenantID, req.Msg.GetMetric(), req.Msg.GetPeriod(), req.Msg.GetNewCap(), req.Msg.GetReason(), claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("override quota cap: %w", err))
	}
	return connect.NewResponse(&adminpb.OverrideQuotaCapResponse{
		TenantId:         result.TenantID,
		Metric:           result.Metric,
		Period:           result.Period,
		PreviousCap:      result.PreviousCap,
		NewCap:           result.NewCap,
		ReservoirVersion: result.ReservoirVersion,
	}), nil
}
