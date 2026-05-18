// Package shorturl adapts app/shorturl onto the Connect transport surface.
package shorturl

import (
	"context"
	"errors"
	"time"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	shorturlapp "github.com/dtszwai/media-processing-service/backend/internal/app/shorturl"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	shorturlpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/shorturl/v1"
)

type Server struct {
	svc *shorturlapp.Service
}

func NewServer(svc *shorturlapp.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Create(ctx context.Context, req *connect.Request[shorturlpb.CreateRequest]) (*connect.Response[shorturlpb.CreateResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetMediaId() == "" || req.Msg.GetAssetId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id and asset_id required"))
	}
	in := shorturlapp.Record{
		TenantID:  claims.TenantID,
		MediaID:   req.Msg.GetMediaId(),
		AssetID:   req.Msg.GetAssetId(),
		Label:     req.Msg.GetLabel(),
		CreatedBy: claims.Subject,
	}
	if req.Msg.ExpiresAt != nil {
		in.ExpiresAt = req.Msg.ExpiresAt.AsTime().Unix()
	}
	code, err := s.svc.Allocate(ctx, in)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("allocate: "+err.Error()))
	}
	out := &shorturlpb.ShortURL{
		Code:      code,
		MediaId:   in.MediaID,
		AssetId:   in.AssetID,
		CreatedAt: timestamppb.New(time.Now().UTC()),
	}
	out.Label = protoutil.OptString(in.Label)
	out.CreatedBy = protoutil.OptString(in.CreatedBy)
	if req.Msg.ExpiresAt != nil {
		out.ExpiresAt = req.Msg.ExpiresAt
	}
	return connect.NewResponse(&shorturlpb.CreateResponse{Url: out}), nil
}

func (s *Server) List(ctx context.Context, req *connect.Request[shorturlpb.ListRequest]) (*connect.Response[shorturlpb.ListResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetMediaId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}
	rows, err := s.svc.ListByMedia(ctx, claims.TenantID, req.Msg.GetMediaId(), req.Msg.GetLimit())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("list: "+err.Error()))
	}
	out := make([]*shorturlpb.ShortURL, len(rows))
	for i, r := range rows {
		su := &shorturlpb.ShortURL{
			Code:    r.Code,
			MediaId: r.MediaID,
			AssetId: r.AssetID,
		}
		su.CreatedAt = protoutil.OptParsedTimestampRFC3339Nano(r.CreatedAt)
		su.Label = protoutil.OptString(r.Label)
		su.CreatedBy = protoutil.OptString(r.CreatedBy)
		if r.ExpiresAt > 0 {
			su.ExpiresAt = timestamppb.New(time.Unix(r.ExpiresAt, 0).UTC())
		}
		su.RevokedAt = protoutil.OptParsedTimestampRFC3339Nano(r.RevokedAt)
		out[i] = su
	}
	return connect.NewResponse(&shorturlpb.ListResponse{Urls: out}), nil
}

func (s *Server) Revoke(ctx context.Context, req *connect.Request[shorturlpb.RevokeRequest]) (*connect.Response[shorturlpb.RevokeResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetCode() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("code required"))
	}
	if err := s.svc.Revoke(ctx, claims.TenantID, req.Msg.GetCode()); err != nil {
		if errors.Is(err, shorturlapp.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("short url not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("revoke: "+err.Error()))
	}
	return connect.NewResponse(&shorturlpb.RevokeResponse{}), nil
}
