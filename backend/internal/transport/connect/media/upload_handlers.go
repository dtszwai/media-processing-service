package media

import (
	"context"
	"errors"
	"strings"

	connect "connectrpc.com/connect"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	mediapb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1"
)

func (s *Server) InitPresignedUpload(ctx context.Context, req *connect.Request[mediapb.InitPresignedUploadRequest]) (*connect.Response[mediapb.InitPresignedUploadResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	ownerUserID := p.UserID
	if req.Msg.GetOwnerUserId() != "" {
		if req.Msg.GetOwnerUserId() != p.UserID && !canAssignOwner(p) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("owner_user_id cannot be assigned by this principal"))
		}
		ownerUserID = req.Msg.GetOwnerUserId()
	}
	out, ierr := s.svc.InitPresignedUpload(ctx, mediaapp.InitInput{
		TenantID:       p.TenantID,
		OwnerUserID:    ownerUserID,
		WebhookURL:     req.Msg.GetWebhookUrl(),
		Filename:       req.Msg.GetFilename(),
		ContentType:    req.Msg.GetContentType(),
		SizeBytes:      req.Msg.GetSizeBytes(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
	})
	if ierr != nil {
		return nil, appError(ierr)
	}
	return connect.NewResponse(initUploadToProto(out)), nil
}

func (s *Server) RefreshPresignedUpload(ctx context.Context, req *connect.Request[mediapb.RefreshPresignedUploadRequest]) (*connect.Response[mediapb.RefreshPresignedUploadResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}
	if _, gerr := s.svc.GetMutable(ctx, p, req.Msg.GetMediaId()); gerr != nil {
		return nil, appError(gerr)
	}
	out, rerr := s.svc.RefreshPresignedUpload(ctx, p.TenantID, req.Msg.GetMediaId())
	if rerr != nil {
		return nil, appError(rerr)
	}
	return connect.NewResponse(refreshUploadToProto(out)), nil
}

func (s *Server) CompletePresignedUpload(ctx context.Context, req *connect.Request[mediapb.CompletePresignedUploadRequest]) (*connect.Response[mediapb.CompletePresignedUploadResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}
	if _, gerr := s.svc.GetMutable(ctx, p, req.Msg.GetMediaId()); gerr != nil {
		return nil, appError(gerr)
	}
	out, cerr := s.svc.CompletePresignedUpload(ctx, mediaapp.CompleteInput{TenantID: p.TenantID, MediaID: req.Msg.GetMediaId()})
	if cerr != nil {
		return nil, appError(cerr)
	}
	return connect.NewResponse(&mediapb.CompletePresignedUploadResponse{
		MediaId:     out.MediaID,
		AssetId:     out.AssetID,
		Lifecycle:   assetLifecycleFromName(out.Lifecycle),
		SizeBytes:   out.SizeBytes,
		ContentType: out.ContentType,
		Etag:        out.ETag,
	}), nil
}
