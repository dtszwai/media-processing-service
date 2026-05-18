package media

import (
	"context"
	"errors"
	"strings"

	connect "connectrpc.com/connect"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	mediapb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1"
)

func (s *Server) ListAssets(ctx context.Context, req *connect.Request[mediapb.ListAssetsRequest]) (*connect.Response[mediapb.ListAssetsResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}
	assets, lerr := s.svc.ListAssetsVisible(ctx, p, req.Msg.GetMediaId())
	if lerr != nil {
		return nil, appError(lerr)
	}
	out := make([]*mediapb.Asset, 0, len(assets))
	for _, asset := range assets {
		out = append(out, assetToProto(asset))
	}
	return connect.NewResponse(&mediapb.ListAssetsResponse{Assets: out}), nil
}

func (s *Server) GetAsset(ctx context.Context, req *connect.Request[mediapb.GetAssetRequest]) (*connect.Response[mediapb.GetAssetResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" || strings.TrimSpace(req.Msg.GetAssetId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id and asset_id required"))
	}
	assets, lerr := s.svc.ListAssetsVisible(ctx, p, req.Msg.GetMediaId())
	if lerr != nil {
		return nil, appError(lerr)
	}
	for _, asset := range assets {
		if asset.ID == req.Msg.GetAssetId() {
			return connect.NewResponse(&mediapb.GetAssetResponse{Asset: assetToProto(asset)}), nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, errors.New("asset not found"))
}

func (s *Server) CreateAssets(ctx context.Context, req *connect.Request[mediapb.CreateAssetsRequest]) (*connect.Response[mediapb.CreateAssetsResponse], error) {
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
	ops := make([]string, 0, len(req.Msg.GetOperations()))
	for _, op := range req.Msg.GetOperations() {
		name := assetOperationToDomain(op)
		if name == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("operations must be supported asset operations"))
		}
		ops = append(ops, name)
	}
	out, cerr := s.svc.CreateAssets(ctx, mediaapp.CreateAssetsInput{
		TenantID:       p.TenantID,
		MediaID:        req.Msg.GetMediaId(),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
		Operations:     ops,
	})
	if cerr != nil {
		return nil, appError(cerr)
	}
	refs := make([]*mediapb.AssetRef, 0, len(out.Assets))
	for _, ref := range out.Assets {
		refs = append(refs, &mediapb.AssetRef{
			Operation: assetOperationFromName(ref.Operation),
			AssetId:   ref.AssetID,
			Lifecycle: assetLifecycleFromName(ref.Lifecycle),
		})
	}
	return connect.NewResponse(&mediapb.CreateAssetsResponse{MediaId: out.MediaID, Assets: refs, Replay: out.Replay}), nil
}

func (s *Server) RetryAsset(ctx context.Context, req *connect.Request[mediapb.RetryAssetRequest]) (*connect.Response[mediapb.RetryAssetResponse], error) {
	p, err := principalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetMediaId()) == "" || strings.TrimSpace(req.Msg.GetAssetId()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id and asset_id required"))
	}
	if _, gerr := s.svc.GetMutable(ctx, p, req.Msg.GetMediaId()); gerr != nil {
		return nil, appError(gerr)
	}
	asset, rerr := s.svc.RetryAsset(ctx, p.TenantID, req.Msg.GetMediaId(), req.Msg.GetAssetId())
	if rerr != nil {
		return nil, appError(rerr)
	}
	return connect.NewResponse(&mediapb.RetryAssetResponse{Asset: assetToProto(*asset)}), nil
}
