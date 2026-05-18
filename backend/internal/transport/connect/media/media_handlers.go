package media

import (
	"context"
	"errors"
	"strings"

	connect "connectrpc.com/connect"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	mediapb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1"
)

func (s *Server) ListMedia(ctx context.Context, req *connect.Request[mediapb.ListMediaRequest]) (*connect.Response[mediapb.ListMediaResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	page, lerr := s.svc.ListByPrincipal(ctx, p, mediaapp.ListOpts{
		Cursor:         req.Msg.GetCursor(),
		Limit:          int(req.Msg.GetLimit()),
		MediaType:      mediaTypeFromProto(req.Msg.GetMediaType()),
		Origin:         originFromProto(req.Msg.GetOrigin()),
		IncludeDeleted: req.Msg.GetIncludeDeleted(),
	})
	if lerr != nil {
		return nil, appError(lerr)
	}
	items := make([]*mediapb.Media, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mediaToProto(item))
	}
	return connect.NewResponse(&mediapb.ListMediaResponse{Items: items, NextCursor: page.NextCursor, HasMore: page.HasMore}), nil
}

func (s *Server) GetMedia(ctx context.Context, req *connect.Request[mediapb.GetMediaRequest]) (*connect.Response[mediapb.GetMediaResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}
	m, gerr := s.svc.GetVisible(ctx, p, req.Msg.GetMediaId())
	if gerr != nil {
		return nil, appError(gerr)
	}
	return connect.NewResponse(&mediapb.GetMediaResponse{Media: mediaToProto(*m)}), nil
}

func (s *Server) DeleteMedia(ctx context.Context, req *connect.Request[mediapb.DeleteMediaRequest]) (*connect.Response[mediapb.DeleteMediaResponse], error) {
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
	if derr := s.svc.SoftDelete(ctx, p.TenantID, req.Msg.GetMediaId()); derr != nil {
		return nil, appError(derr)
	}
	return connect.NewResponse(&mediapb.DeleteMediaResponse{}), nil
}
