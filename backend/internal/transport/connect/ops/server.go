// Package ops adapts internal/app/ops onto the Connect transport surface.
// The handler is mounted on the API process only when LOCAL_ONLY=true; see
// cmd/api/routes.go.
package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	opsapp "github.com/dtszwai/media-processing-service/backend/internal/app/ops"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	opsv1 "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/ops/v1"
)

// Server adapts *opsapp.Service onto the OpsService Connect handler.
type Server struct {
	svc *opsapp.Service
}

func NewServer(svc *opsapp.Service) *Server { return &Server{svc: svc} }

func (s *Server) ListJobs(ctx context.Context, req *connect.Request[opsv1.ListJobsRequest]) (*connect.Response[opsv1.ListJobsResponse], error) {
	rows, cursor, err := s.svc.ListJobs(ctx, opsapp.ListJobsFilter{
		TenantID:   req.Msg.GetTenantId(),
		Status:     req.Msg.GetStatus(),
		OutputType: req.Msg.GetOutputType(),
		Limit:      req.Msg.GetLimit(),
		Cursor:     req.Msg.GetCursor(),
	})
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.ListJobsResponse{NextCursor: cursor}
	for _, r := range rows {
		out.Jobs = append(out.Jobs, jobSummaryToProto(r))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) GetJob(ctx context.Context, req *connect.Request[opsv1.GetJobRequest]) (*connect.Response[opsv1.GetJobResponse], error) {
	view, err := s.svc.GetJob(ctx, req.Msg.GetJobId())
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.GetJobResponse{View: &opsv1.FullJobView{
		Summary:                 jobSummaryToProto(view.Summary),
		Job:                     mapToStruct(view.JobAttrs),
		Media:                   mapToStruct(view.MediaAttrs),
		ResultAsset:             mapToStruct(view.AssetAttrs),
		RelatedKeys:             view.RelatedKeys,
		FirstEventAt:            protoutil.OptTimestamp(view.FirstEventAt),
		LastEventAt:             protoutil.OptTimestamp(view.LastEventAt),
		DecryptedPrompt:         view.DecryptedPrompt,
		DecryptedPreparedPrompt: view.DecryptedPreparedPrompt,
	}}
	for _, sp := range view.Spans {
		out.View.Spans = append(out.View.Spans, traceSpanToProto(sp))
	}
	if view.GateDecision != nil {
		out.View.GateDecision = gateDecisionToProto(view.GateDecision)
	}
	return connect.NewResponse(out), nil
}

func (s *Server) ListMedia(ctx context.Context, req *connect.Request[opsv1.ListMediaRequest]) (*connect.Response[opsv1.ListMediaResponse], error) {
	rows, cursor, err := s.svc.ListMedia(ctx, opsapp.ListMediaFilter{
		TenantID:       req.Msg.GetTenantId(),
		MediaType:      req.Msg.GetMediaType(),
		Origin:         req.Msg.GetOrigin(),
		Lifecycle:      req.Msg.GetLifecycle(),
		IncludeDeleted: req.Msg.GetIncludeDeleted(),
		Limit:          req.Msg.GetLimit(),
		Cursor:         req.Msg.GetCursor(),
	})
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.ListMediaResponse{NextCursor: cursor}
	for _, r := range rows {
		out.Items = append(out.Items, mediaRowToProto(r))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) GetMedia(ctx context.Context, req *connect.Request[opsv1.GetMediaRequest]) (*connect.Response[opsv1.GetMediaResponse], error) {
	mediaID := req.Msg.GetMediaId()
	if mediaID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}
	tenantID, _, ok := s.svc.LocateMedia(ctx, mediaID)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("media %q not found", mediaID))
	}
	view, gerr := s.svc.GetMedia(ctx, tenantID, mediaID)
	if gerr != nil {
		return nil, toConnect(gerr)
	}
	out := &opsv1.GetMediaResponse{View: &opsv1.FullMediaView{
		Row:   mediaRowToProto(view.Row),
		Media: mapToStruct(view.Media),
	}}
	if view.JobID != "" {
		out.View.JobId = &view.JobID
	}
	for _, a := range view.Assets {
		out.View.Assets = append(out.View.Assets, mapToStruct(a))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) ScanDdb(ctx context.Context, req *connect.Request[opsv1.ScanDdbRequest]) (*connect.Response[opsv1.ScanDdbResponse], error) {
	rows, cursor, err := s.svc.ScanDdb(ctx, opsapp.ScanDdbFilter{
		PKPrefix: req.Msg.GetPkPrefix(),
		SKPrefix: req.Msg.GetSkPrefix(),
		Limit:    req.Msg.GetLimit(),
		Cursor:   req.Msg.GetCursor(),
	})
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.ScanDdbResponse{NextCursor: cursor}
	for _, r := range rows {
		out.Rows = append(out.Rows, ddbRowToProto(r))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) GetDdbRow(ctx context.Context, req *connect.Request[opsv1.GetDdbRowRequest]) (*connect.Response[opsv1.GetDdbRowResponse], error) {
	row, err := s.svc.GetDdbRow(ctx, req.Msg.GetPk(), req.Msg.GetSk())
	if err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.GetDdbRowResponse{Row: ddbRowToProto(*row)}), nil
}

func (s *Server) QueueDepths(ctx context.Context, _ *connect.Request[opsv1.QueueDepthsRequest]) (*connect.Response[opsv1.QueueDepthsResponse], error) {
	stats, err := s.svc.QueueDepths(ctx)
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.QueueDepthsResponse{}
	for _, st := range stats {
		out.Queues = append(out.Queues, &opsv1.QueueStat{
			Name:                     st.Name,
			Url:                      st.URL,
			Visible:                  st.Visible,
			InFlight:                 st.InFlight,
			Delayed:                  st.Delayed,
			VisibilityTimeoutSeconds: st.VisibilityTimeoutSecs,
			OldestMessageAgeSeconds:  st.OldestMessageAgeSecs,
			DlqName:                  st.DLQName,
			DlqCount:                 st.DLQCount,
			TierClass:                st.TierClass,
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Server) GetTenantUsage(ctx context.Context, req *connect.Request[opsv1.GetTenantUsageRequest]) (*connect.Response[opsv1.GetTenantUsageResponse], error) {
	view, err := s.svc.GetTenantUsage(ctx, req.Msg.GetTenantId())
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.GetTenantUsageResponse{
		TenantId:           view.TenantID,
		CurrentDailyPeriod: view.CurrentDailyPeriod,
		DailyCost:          tenantUsageReservoirToProto(*view.DailyCost),
	}
	for _, r := range view.Reservoirs {
		out.Reservoirs = append(out.Reservoirs, tenantUsageReservoirToProto(r))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) ListS3(ctx context.Context, req *connect.Request[opsv1.ListS3Request]) (*connect.Response[opsv1.ListS3Response], error) {
	nodes, err := s.svc.ListS3(ctx, req.Msg.GetPrefix(), req.Msg.GetDelimiter(), req.Msg.GetLimit())
	if err != nil {
		return nil, toConnect(err)
	}
	out := &opsv1.ListS3Response{}
	for _, n := range nodes {
		node := &opsv1.S3Node{
			Key:       n.Key,
			Name:      n.Name,
			IsPrefix:  n.IsPrefix,
			SizeBytes: n.SizeBytes,
			Etag:      n.ETag,
		}
		if !n.LastModified.IsZero() {
			node.LastModified = timestamppb.New(n.LastModified)
		}
		out.Nodes = append(out.Nodes, node)
	}
	return connect.NewResponse(out), nil
}

func (s *Server) PresignDownload(ctx context.Context, req *connect.Request[opsv1.PresignDownloadRequest]) (*connect.Response[opsv1.PresignDownloadResponse], error) {
	url, expires, err := s.svc.PresignDownload(ctx, req.Msg.GetKey())
	if err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.PresignDownloadResponse{
		Url:       url,
		ExpiresAt: timestamppb.New(expires),
	}), nil
}

func (s *Server) StreamLogs(ctx context.Context, req *connect.Request[opsv1.StreamLogsRequest], stream *connect.ServerStream[opsv1.StreamLogsResponse]) error {
	filter := opsapp.LogFilter{
		Service:         req.Msg.GetService(),
		JobID:           req.Msg.GetJobId(),
		MediaID:         req.Msg.GetMediaId(),
		Level:           req.Msg.GetLevel(),
		Contains:        req.Msg.GetContains(),
		TailLines:       req.Msg.GetTailLines(),
		LookbackSeconds: req.Msg.GetLookbackSeconds(),
	}
	if s.svc.Loki == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("ops: loki not wired"))
	}
	// Initial backfill, then 2s tail. The cursor is the latest emitted
	// timestamp; new pages are filtered to lines strictly after it so the
	// client never sees a duplicate.
	since := time.Time{}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		lines, err := s.svc.Loki.StreamLogs(ctx, filter, since)
		if err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
		nextSince := since
		for _, l := range lines {
			if !l.Timestamp.After(since) {
				continue
			}
			msg := &opsv1.StreamLogsResponse{Line: &opsv1.LogLine{
				Ts:      timestamppb.New(l.Timestamp),
				Service: l.Service,
				Level:   l.Level,
				Body:    l.Body,
				Labels:  l.Labels,
			}}
			if err := stream.Send(msg); err != nil {
				return err
			}
			if l.Timestamp.After(nextSince) {
				nextSince = l.Timestamp
			}
		}
		since = nextSince
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Server) CancelJob(ctx context.Context, req *connect.Request[opsv1.CancelJobRequest]) (*connect.Response[opsv1.CancelJobResponse], error) {
	if err := s.svc.CancelJob(ctx, req.Msg.GetJobId(), req.Msg.GetReason()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.CancelJobResponse{}), nil
}

func (s *Server) RetryJob(ctx context.Context, req *connect.Request[opsv1.RetryJobRequest]) (*connect.Response[opsv1.RetryJobResponse], error) {
	if err := s.svc.RetryJob(ctx, req.Msg.GetJobId()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.RetryJobResponse{}), nil
}

func (s *Server) ForceFailJob(ctx context.Context, req *connect.Request[opsv1.ForceFailJobRequest]) (*connect.Response[opsv1.ForceFailJobResponse], error) {
	if err := s.svc.ForceFailJob(ctx, req.Msg.GetJobId(), req.Msg.GetErrorCode(), req.Msg.GetErrorMessage()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.ForceFailJobResponse{}), nil
}

func (s *Server) ReplayOutbox(ctx context.Context, req *connect.Request[opsv1.ReplayOutboxRequest]) (*connect.Response[opsv1.ReplayOutboxResponse], error) {
	if err := s.svc.ReplayOutbox(ctx, req.Msg.GetJobId()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.ReplayOutboxResponse{}), nil
}

func (s *Server) PurgeQueue(ctx context.Context, req *connect.Request[opsv1.PurgeQueueRequest]) (*connect.Response[opsv1.PurgeQueueResponse], error) {
	if err := s.svc.PurgeQueue(ctx, req.Msg.GetQueueName()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.PurgeQueueResponse{}), nil
}

func (s *Server) RedriveDlq(ctx context.Context, req *connect.Request[opsv1.RedriveDlqRequest]) (*connect.Response[opsv1.RedriveDlqResponse], error) {
	moved, failed, err := s.svc.RedriveDlq(ctx, req.Msg.GetDlqName(), req.Msg.GetLimit())
	if err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.RedriveDlqResponse{Moved: moved, Failed: failed}), nil
}

func (s *Server) PutDdbAttr(ctx context.Context, req *connect.Request[opsv1.PutDdbAttrRequest]) (*connect.Response[opsv1.PutDdbAttrResponse], error) {
	if err := s.svc.PutDdbAttr(ctx, req.Msg.GetPk(), req.Msg.GetSk(), req.Msg.GetAttributeName(), req.Msg.GetValueJson()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.PutDdbAttrResponse{}), nil
}

func (s *Server) DeleteDdbRow(ctx context.Context, req *connect.Request[opsv1.DeleteDdbRowRequest]) (*connect.Response[opsv1.DeleteDdbRowResponse], error) {
	if err := s.svc.DeleteDdbRow(ctx, req.Msg.GetPk(), req.Msg.GetSk()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.DeleteDdbRowResponse{}), nil
}

func (s *Server) DeleteS3Object(ctx context.Context, req *connect.Request[opsv1.DeleteS3ObjectRequest]) (*connect.Response[opsv1.DeleteS3ObjectResponse], error) {
	if err := s.svc.DeleteS3Object(ctx, req.Msg.GetKey()); err != nil {
		return nil, toConnect(err)
	}
	return connect.NewResponse(&opsv1.DeleteS3ObjectResponse{}), nil
}

func (s *Server) ListGenerationModels(_ context.Context, _ *connect.Request[opsv1.ListGenerationModelsRequest]) (*connect.Response[opsv1.ListGenerationModelsResponse], error) {
	out := &opsv1.ListGenerationModelsResponse{}
	for _, e := range s.svc.GenerationCatalog {
		out.Providers = append(out.Providers, &opsv1.GenerationProviderModels{
			OutputType:   e.OutputType,
			Provider:     e.Provider,
			Models:       append([]string(nil), e.Models...),
			DefaultModel: e.DefaultModel,
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Server) GetLocalIdentity(_ context.Context, _ *connect.Request[opsv1.GetLocalIdentityRequest]) (*connect.Response[opsv1.GetLocalIdentityResponse], error) {
	return connect.NewResponse(&opsv1.GetLocalIdentityResponse{
		TenantId: s.svc.LocalTenantID,
		UserId:   s.svc.LocalUserID,
	}), nil
}
