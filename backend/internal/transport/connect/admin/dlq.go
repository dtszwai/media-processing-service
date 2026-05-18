package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	connect "connectrpc.com/connect"
	"golang.org/x/sync/errgroup"

	adminapp "github.com/dtszwai/media-processing-service/backend/internal/app/admin"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	adminpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1"
)

func (s *Server) GetDLQStatus(ctx context.Context, _ *connect.Request[adminpb.GetDLQStatusRequest]) (*connect.Response[adminpb.GetDLQStatusResponse], error) {
	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	rows, err := s.dlq.Status(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("status: %w", err))
	}
	out := make([]*adminpb.DLQQueue, 0, len(rows))
	for _, r := range rows {
		out = append(out, &adminpb.DLQQueue{
			Name:                    r.Name,
			QueueUrl:                r.URL,
			SourceQueueUrl:          r.SourceURL,
			ApproximateMessageCount: r.Count,
		})
	}
	return connect.NewResponse(&adminpb.GetDLQStatusResponse{Queues: out}), nil
}

func (s *Server) ListDLQMessages(ctx context.Context, req *connect.Request[adminpb.ListDLQMessagesRequest]) (*connect.Response[adminpb.ListDLQMessagesResponse], error) {
	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	if req.Msg.GetDlqName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dlq_name required"))
	}
	rows, err := s.dlq.Peek(ctx, req.Msg.GetDlqName(), req.Msg.GetLimit())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("peek: %w", err))
	}
	out := make([]*adminpb.DLQMessage, len(rows))
	for i, m := range rows {
		msg := &adminpb.DLQMessage{
			MessageId:         m.ID,
			ReceiptHandle:     m.ReceiptHandle,
			Body:              m.Body,
			MessageAttributes: m.MessageAttributes,
			BodySignature:     m.BodySignature,
		}
		if v, ok := m.Attributes["SentTimestamp"]; ok {
			msg.SentTimestamp = protoutil.PtrString(v)
		}
		if v, ok := m.Attributes["ApproximateReceiveCount"]; ok {
			n, _ := strconv.Atoi(v)
			msg.ApproximateReceiveCount = protoutil.PtrInt32(int32(n))
		}
		out[i] = msg
	}
	return connect.NewResponse(&adminpb.ListDLQMessagesResponse{Messages: out}), nil
}

func (s *Server) ReplayDLQMessages(ctx context.Context, req *connect.Request[adminpb.ReplayDLQMessagesRequest]) (*connect.Response[adminpb.ReplayDLQMessagesResponse], error) {
	claims, err := authz.RequirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetDlqName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dlq_name required"))
	}
	msgs := req.Msg.GetMessages()
	results := make([]*adminpb.DLQReplayResult, len(msgs))
	var mu sync.Mutex
	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(3)
	for i, m := range msgs {
		i, m := i, m
		eg.Go(func() error {
			res := &adminpb.DLQReplayResult{OriginalMessageId: m.GetMessageId()}
			newID, err := s.dlq.ReplayAs(gctx, req.Msg.GetDlqName(), claims.TenantID, claims.Subject, adminapp.DLQMessageInput{
				ID:                m.GetMessageId(),
				ReceiptHandle:     m.GetReceiptHandle(),
				Body:              m.GetBody(),
				MessageAttributes: m.GetMessageAttributes(),
				BodySignature:     m.GetBodySignature(),
			})
			if err != nil {
				res.Failure = classifyReplayErr(err)
			} else {
				res.NewMessageId = protoutil.PtrString(newID)
			}
			mu.Lock()
			results[i] = res
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	return connect.NewResponse(&adminpb.ReplayDLQMessagesResponse{Results: results}), nil
}

func (s *Server) DeleteDLQMessage(ctx context.Context, req *connect.Request[adminpb.DeleteDLQMessageRequest]) (*connect.Response[adminpb.DeleteDLQMessageResponse], error) {
	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	if req.Msg.GetDlqName() == "" || req.Msg.GetReceiptHandle() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dlq_name and receipt_handle required"))
	}
	if err := s.dlq.Delete(ctx, req.Msg.GetDlqName(), req.Msg.GetReceiptHandle()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete: %w", err))
	}
	return connect.NewResponse(&adminpb.DeleteDLQMessageResponse{}), nil
}

func (s *Server) PurgeDLQ(ctx context.Context, req *connect.Request[adminpb.PurgeDLQRequest]) (*connect.Response[adminpb.PurgeDLQResponse], error) {
	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		return nil, err
	}
	if req.Msg.GetDlqName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("dlq_name required"))
	}
	if err := s.dlq.Purge(ctx, req.Msg.GetDlqName()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("purge: %w", err))
	}
	return connect.NewResponse(&adminpb.PurgeDLQResponse{}), nil
}

func classifyReplayErr(err error) *adminpb.DLQReplayFailure {
	msg := err.Error()
	switch {
	case errors.Is(err, adminapp.ErrDLQReplayInFlight):
		return &adminpb.DLQReplayFailure{ErrorCode: "REPLAY_IN_FLIGHT", ErrorMessage: msg, Retryable: true}
	case errors.Is(err, adminapp.ErrDLQReplayConflict):
		return &adminpb.DLQReplayFailure{ErrorCode: "REPLAY_CONFLICT", ErrorMessage: msg, Retryable: false}
	case errors.Is(err, adminapp.ErrDLQReplayInvalidSignature):
		return &adminpb.DLQReplayFailure{ErrorCode: "INVALID_SIGNATURE", ErrorMessage: msg, Retryable: false}
	case errors.Is(err, adminapp.ErrDLQReplayDeleteAfterSend):
		return &adminpb.DLQReplayFailure{ErrorCode: "DELETE_FAILED", ErrorMessage: msg, Retryable: true}
	case strings.Contains(msg, "unknown dlq"):
		return &adminpb.DLQReplayFailure{ErrorCode: "UNKNOWN_DLQ", ErrorMessage: msg, Retryable: false}
	case errors.Is(err, adminapp.ErrDLQReplaySend):
		return &adminpb.DLQReplayFailure{ErrorCode: "SEND_FAILED", ErrorMessage: msg, Retryable: true}
	default:
		return &adminpb.DLQReplayFailure{ErrorCode: "SEND_PERMANENT_FAILURE", ErrorMessage: msg, Retryable: false}
	}
}
