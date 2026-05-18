package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminapp "github.com/dtszwai/media-processing-service/backend/internal/app/admin"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) InspectIdempotencyClaim(ctx context.Context, req *connect.Request[adminpb.InspectIdempotencyClaimRequest]) (*connect.Response[adminpb.InspectIdempotencyClaimResponse], error) {
	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	if req.Msg.GetScope() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("scope required"))
	}
	claim, err := s.idempotency.Inspect(ctx, req.Msg.GetScope())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("inspect idempotency claim: %w", err))
	}
	return connect.NewResponse(&adminpb.InspectIdempotencyClaimResponse{Claim: idempotencyClaimToProto(claim)}), nil
}

func (s *Server) ResetIdempotencyClaim(ctx context.Context, req *connect.Request[adminpb.ResetIdempotencyClaimRequest]) (*connect.Response[adminpb.ResetIdempotencyClaimResponse], error) {
	claims, err := authz.RequirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetScope() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("scope and reason required"))
	}
	resetGeneration, err := s.idempotency.Reset(ctx, req.Msg.GetScope(), req.Msg.GetReason(), claims.TenantID, claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reset idempotency claim: %w", err))
	}
	return connect.NewResponse(&adminpb.ResetIdempotencyClaimResponse{Scope: req.Msg.GetScope(), ResetGeneration: resetGeneration}), nil
}

func idempotencyClaimToProto(claim adminapp.IdempotencyClaim) *adminpb.IdempotencyClaim {
	out := &adminpb.IdempotencyClaim{
		Scope:     claim.Scope,
		Status:    claim.Status,
		InputHash: claim.InputHash,
		ResultRef: claim.ResultRef,
	}
	if !claim.ClaimedAt.IsZero() {
		out.ClaimedAt = timestampOpt(claim.ClaimedAt)
	}
	if !claim.CompletedAt.IsZero() {
		out.CompletedAt = timestampOpt(claim.CompletedAt)
	}
	if !claim.TTLAt.IsZero() {
		out.TtlAt = timestampOpt(claim.TTLAt)
	}
	if claim.Attempts > 0 {
		attempts := claim.Attempts
		out.Attempts = &attempts
	}
	if claim.LastError != "" {
		out.LastError = protoutil.PtrString(claim.LastError)
	}
	return out
}

func timestampOpt(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t.UTC())
}
