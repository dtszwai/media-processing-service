package media

import (
	"context"
	"errors"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	domainmedia "github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	mediapb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1"
)

func (s *Server) PresignAssetDownload(ctx context.Context, req *connect.Request[mediapb.PresignAssetDownloadRequest]) (*connect.Response[mediapb.PresignAssetDownloadResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" || strings.TrimSpace(req.Msg.GetAssetId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id and asset_id required"))
	}
	ttl := ttlFromSeconds(req.Msg.GetTtlSeconds())
	url, perr := s.svc.PresignAssetDownloadVisible(ctx, p, req.Msg.GetMediaId(), req.Msg.GetAssetId(), ttl)
	if perr != nil {
		return nil, appError(perr)
	}
	s.emitAnalytics(ctx, analyticsapp.Event{
		EventType:   analyticsapp.EventTypeMediaDownload,
		TenantID:    p.TenantID,
		MediaID:     req.Msg.GetMediaId(),
		AssetID:     req.Msg.GetAssetId(),
		PrincipalID: principalID(p),
	})
	return connect.NewResponse(&mediapb.PresignAssetDownloadResponse{Url: url, ExpiresIn: uint32(ttl.Seconds()), ExpiresAt: timestamppb.New(time.Now().UTC().Add(ttl))}), nil
}

func (s *Server) GetMediaRoleURL(ctx context.Context, req *connect.Request[mediapb.GetMediaRoleURLRequest]) (*connect.Response[mediapb.GetMediaRoleURLResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" || req.Msg.GetRole() == mediapb.AssetRole_ASSET_ROLE_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id and role required"))
	}
	role := assetRoleToDomain(req.Msg.GetRole())
	if role == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported role"))
	}
	ttl := ttlFromSeconds(req.Msg.GetTtlSeconds())
	out, rerr := s.svc.GetRoleURLVisible(ctx, p, req.Msg.GetMediaId(), role, ttl)
	if rerr != nil {
		return nil, appError(rerr)
	}
	s.emitAnalytics(ctx, analyticsapp.Event{
		EventType:   roleEventType(role),
		TenantID:    p.TenantID,
		MediaID:     req.Msg.GetMediaId(),
		AssetID:     out.AssetID,
		PrincipalID: principalID(p),
	})
	return connect.NewResponse(&mediapb.GetMediaRoleURLResponse{
		AssetId:     out.AssetID,
		Url:         out.URL,
		ExpiresAt:   timestamppb.New(out.ExpiresAt),
		ContentType: out.ContentType,
		SizeBytes:   out.SizeBytes,
	}), nil
}

func ttlFromSeconds(seconds uint32) time.Duration {
	if seconds == 0 || seconds > 3600 {
		return defaultDownloadTTL
	}
	return time.Duration(seconds) * time.Second
}

func roleEventType(role domainmedia.AssetRole) analyticsapp.EventType {
	if role == domainmedia.AssetRoleDownload {
		return analyticsapp.EventTypeMediaDownload
	}
	return analyticsapp.EventTypeMediaView
}
