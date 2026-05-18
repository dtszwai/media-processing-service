package admin

import (
	"context"
	"errors"
	"fmt"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) RotateWebhookSecret(ctx context.Context, req *connect.Request[adminpb.RotateWebhookSecretRequest]) (*connect.Response[adminpb.RotateWebhookSecretResponse], error) {
	claims, err := authz.RequireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	tenantID, err := tenantIDForAdminRequest(claims, req.Msg.GetTenantId(), false)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("reason required"))
	}
	oldKeyID, newKeyID, err := s.webhook.RotateSecret(ctx, tenantID, req.Msg.GetEndpointId(), req.Msg.GetReason(), claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rotate webhook secret: %w", err))
	}
	return connect.NewResponse(&adminpb.RotateWebhookSecretResponse{TenantId: tenantID, EndpointId: req.Msg.GetEndpointId(), OldSecretKeyId: oldKeyID, NewSecretKeyId: newKeyID}), nil
}
