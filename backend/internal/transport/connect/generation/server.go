// Package generation adapts app/generation onto the Connect transport surface.
package generation

import (
	"context"
	"log/slog"
	"time"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	generationapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// ResultPresigner returns a presigned URL for a result asset.
type ResultPresigner interface {
	PresignResult(ctx context.Context, tenantID, mediaID, assetID string) (string, time.Time, error)
}

type OutputRollupReader interface {
	GetOutputRollup(ctx context.Context, tenantID, jobID string) (*domaingen.Output, []domaingen.Variant, error)
}

type Server struct {
	repo         generationapp.JobRepository
	submissions  *generationapp.SubmissionService
	outputReader OutputRollupReader
	presigner    ResultPresigner
	tracker      analyticsapp.Tracker
	now          func() time.Time
}

func NewServer(repo generationapp.JobRepository, submissions *generationapp.SubmissionService, presigner ResultPresigner, tracker analyticsapp.Tracker) *Server {
	if submissions == nil {
		panic("connect/generation: submissions service required")
	}
	s := &Server{repo: repo, submissions: submissions, presigner: presigner, tracker: tracker, now: time.Now}
	if reader, ok := repo.(OutputRollupReader); ok {
		s.outputReader = reader
	}
	return s
}

func (s *Server) emitAnalytics(ctx context.Context, evt analyticsapp.Event) {
	if s.tracker == nil {
		return
	}
	if err := s.tracker.Track(ctx, evt); err != nil {
		slog.WarnContext(ctx, "analytics track failed",
			"event_type", string(evt.EventType),
			"tenant_id", evt.TenantID,
			"media_id", evt.MediaID,
			"err", err.Error(),
		)
	}
}
