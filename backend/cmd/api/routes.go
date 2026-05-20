package main

import (
	"net/http"

	authapp "github.com/dtszwai/media-processing-service/backend/internal/app/auth"
	generationapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	connectadmin "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/admin"
	connectanalytics "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/analytics"
	connectauth "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/auth"
	connectgeneration "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/generation"
	connectmedia "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/media"
	connectshorturl "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/shorturl"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
	adminconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1/adminv1connect"
	analyticsconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/analytics/v1/analyticsv1connect"
	authconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/auth/v1/authv1connect"
	generationconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1/generationv1connect"
	mediaconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1/mediav1connect"
	shorturlconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/shorturl/v1/shorturlv1connect"
)

// registerConnectAPIs mounts the generated Connect procedures. Clients use
// Connect JSON or binary protobuf against the canonical service paths.
func registerConnectAPIs(mux *http.ServeMux, a *bootstrap.AWS, jwtSecret []byte) {
	authSvc := authapp.NewService(a.Users, a.APIKeys, authapp.Config{
		JWTSecret: jwtSecret,
		IDGen:     randid.New,
		Recorder:  a.AuditRecorder,
	})
	authServer := connectauth.NewServer(authSvc)
	shorturlServer := connectshorturl.NewServer(a.ShortURL)
	adminServer := connectadmin.NewServer(connectadmin.Config{
		DLQ:         a.DLQ,
		OutboxDLQ:   a.OutboxDLQ,
		Idempotency: a.IdempotencyAdmin,
		Quota:       a.QuotaAdmin,
		Webhook:     a.WebhookAdmin,
		Tenant:      a.TenantAdmin,
		Workflow:    a.WorkflowAdmin,
	})

	mediaSvc := mediaapp.NewService(a.MediaRepo, a.Blob)
	mediaSvc.Quota = a.QuotaMeter
	mediaSvc.Derive = a.MediaRepo
	presigner := &resultPresigner{svc: mediaSvc}
	submissions := &generationapp.SubmissionService{
		Submitter:    a.JobRepo,
		ReplayReader: a.JobRepo,
		Idempotency:  a.Idempotency,
		CapacityHint: a.QuotaAdapter,
		Instruments:  a.Instruments,
		NewID:        randid.New,
	}
	generationServer := connectgeneration.NewServer(a.JobRepo, submissions, presigner, a.AnalyticsTracker)
	analyticsServer := connectanalytics.NewServer(a.AnalyticsReader)

	path, handler := authconnect.NewAuthServiceHandler(authServer)
	mux.Handle(path, handler)
	path, handler = shorturlconnect.NewShortURLServiceHandler(shorturlServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewDLQAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewOutboxDLQAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewIdempotencyAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewQuotaAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewWebhookAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewTenantAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = adminconnect.NewWorkflowAdminServiceHandler(adminServer)
	mux.Handle(path, handler)
	path, handler = analyticsconnect.NewAnalyticsServiceHandler(analyticsServer)
	mux.Handle(path, handler)
	path, handler = generationconnect.NewGenerationServiceHandler(generationServer)
	mux.Handle(path, handler)
	path, handler = mediaconnect.NewMediaServiceHandler(connectmedia.NewServer(mediaSvc, a.AnalyticsTracker))
	mux.Handle(path, handler)
}
