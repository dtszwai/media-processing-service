package admin

import (
	"context"
	"errors"
	"fmt"

	connect "connectrpc.com/connect"

	adminapp "github.com/dtszwai/media-processing-service/backend/internal/app/admin"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) ListOutboxDlqRows(ctx context.Context, req *connect.Request[adminpb.ListOutboxDlqRowsRequest]) (*connect.Response[adminpb.ListOutboxDlqRowsResponse], error) {
	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.outboxDLQ.List(ctx, req.Msg.GetStream(), req.Msg.GetLimit())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list outbox dlq: %w", err))
	}
	out := make([]*adminpb.OutboxDLQRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, outboxDLQRowToProto(row))
	}
	return connect.NewResponse(&adminpb.ListOutboxDlqRowsResponse{Rows: out}), nil
}

func (s *Server) ReplayOutboxDlqRow(ctx context.Context, req *connect.Request[adminpb.ReplayOutboxDlqRowRequest]) (*connect.Response[adminpb.ReplayOutboxDlqRowResponse], error) {
	claims, err := authz.RequirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	oldID, newID, err := s.outboxDLQ.Replay(ctx, req.Msg.GetStream(), req.Msg.GetEventId(), req.Msg.GetReason(), claims.TenantID, claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("replay outbox dlq: %w", err))
	}
	return connect.NewResponse(&adminpb.ReplayOutboxDlqRowResponse{ReplayedFromEventId: oldID, NewEventId: newID}), nil
}

func (s *Server) AbandonOutboxDlqRow(ctx context.Context, req *connect.Request[adminpb.AbandonOutboxDlqRowRequest]) (*connect.Response[adminpb.AbandonOutboxDlqRowResponse], error) {
	claims, err := authz.RequirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.outboxDLQ.Abandon(ctx, req.Msg.GetStream(), req.Msg.GetEventId(), req.Msg.GetReason(), claims.TenantID, claims.Subject); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("abandon outbox dlq: %w", err))
	}
	return connect.NewResponse(&adminpb.AbandonOutboxDlqRowResponse{}), nil
}

func (s *Server) DeleteOutboxDlqRow(ctx context.Context, req *connect.Request[adminpb.DeleteOutboxDlqRowRequest]) (*connect.Response[adminpb.DeleteOutboxDlqRowResponse], error) {
	claims, err := authz.RequirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetStream() == "" || req.Msg.GetEventId() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("stream, event_id, and reason required"))
	}
	if err := s.outboxDLQ.Delete(ctx, req.Msg.GetStream(), req.Msg.GetEventId(), req.Msg.GetReason(), claims.TenantID, claims.Subject); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete outbox dlq: %w", err))
	}
	return connect.NewResponse(&adminpb.DeleteOutboxDlqRowResponse{}), nil
}

func (s *Server) PurgeOutboxDlqRows(ctx context.Context, req *connect.Request[adminpb.PurgeOutboxDlqRowsRequest]) (*connect.Response[adminpb.PurgeOutboxDlqRowsResponse], error) {
	claims, err := authz.RequirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetStream() == "" || req.Msg.GetReason() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("stream and reason required"))
	}
	deleted, err := s.outboxDLQ.Purge(ctx, req.Msg.GetStream(), req.Msg.GetReason(), claims.TenantID, claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("purge outbox dlq: %w", err))
	}
	return connect.NewResponse(&adminpb.PurgeOutboxDlqRowsResponse{DeletedCount: deleted}), nil
}

func outboxDLQRowToProto(row adminapp.OutboxDLQRow) *adminpb.OutboxDLQRow {
	out := &adminpb.OutboxDLQRow{
		Stream:    row.Stream,
		Shard:     row.Shard,
		EventId:   row.EventID,
		EventType: row.EventType,
		TenantId:  row.TenantID,
		Body:      row.Body,
		Attempts:  row.Attempts,
	}
	if row.LastError != "" {
		out.LastError = protoutil.PtrString(row.LastError)
	}
	if !row.PartitionTS.IsZero() {
		out.PartitionTs = protoutil.OptTimestamp(row.PartitionTS)
	}
	if !row.FirstFailedAt.IsZero() {
		out.FirstFailedAt = protoutil.OptTimestamp(row.FirstFailedAt)
	}
	if !row.LastFailedAt.IsZero() {
		out.LastFailedAt = protoutil.OptTimestamp(row.LastFailedAt)
	}
	return out
}
