package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	authapp "github.com/dtszwai/media-processing-service/backend/internal/app/auth"
	generationapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	opsapp "github.com/dtszwai/media-processing-service/backend/internal/app/ops"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	connectadmin "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/admin"
	connectanalytics "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/analytics"
	connectauth "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/auth"
	connectgeneration "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/generation"
	connectmedia "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/media"
	connectops "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/ops"
	connectshorturl "github.com/dtszwai/media-processing-service/backend/internal/transport/connect/shorturl"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
	adminconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/admin/v1/adminv1connect"
	analyticsconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/analytics/v1/analyticsv1connect"
	authconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/auth/v1/authv1connect"
	generationconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1/generationv1connect"
	mediaconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1/mediav1connect"
	opsconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/ops/v1/opsv1connect"
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

	// Operator console — LOCAL_ONLY surface. Production deploys MUST leave
	// LOCAL_ONLY unset; the handler reaches into infra (raw DDB, SQS, S3,
	// Loki) in ways the public API never should.
	if os.Getenv("LOCAL_ONLY") == "true" {
		opsSvc := buildOpsService(a)
		path, handler = opsconnect.NewOpsServiceHandler(connectops.NewServer(opsSvc))
		mux.Handle(path, handler)
	}
}

// buildOpsService wires the operator console service onto bootstrap.AWS
// resources. Kept in this file so the DLQ topology, queue urls, and the
// local-user/tenant defaults all flow from the same assembly root.
func buildOpsService(a *bootstrap.AWS) *opsapp.Service {
	svc := opsapp.New(a.KV, a.Blob, a.SQS, a.Table, a.Bucket, a.JobRepo)
	if c, ok := opsAWSClient(a); ok {
		svc.DDB = c
	}
	namedQueues := map[string]string{
		"media-events":        a.MediaQueueURL,
		"media-upload-events": a.MediaUploadEventsQueueURL,
		"media-cleanup":       a.MediaCleanupQueueURL,
		"webhook":             a.WebhookQueueURL,
		"analytics":           a.AnalyticsQueueURL,
	}
	for name, url := range namedQueues {
		if url == "" {
			continue
		}
		svc.QueueURLs[name] = url
	}
	for name, url := range a.GenerationQueues {
		svc.QueueURLs[name] = url
	}
	for name, info := range a.DLQs {
		svc.DLQs[name] = opsapp.DLQInfo{Name: info.Name, URL: info.URL, SourceURL: info.SourceURL}
	}
	if lokiURL := os.Getenv("LOKI_URL"); lokiURL != "" {
		svc.Loki = opsapp.NewLokiClient(lokiURL)
	}
	svc.LocalTenantID = envOr("LOCAL_TENANT_ID", "tenant_local")
	svc.LocalUserID = envOr("LOCAL_USER_ID", "user_local")
	svc.TenantCostCapMicroUSD = a.TenantCostCapMicroUSD
	// Default logger so audit-write failures aren't silent. cmd/api/main
	// initialised slog.Default() during runtime.Init.
	svc.Logger = slog.Default()
	for _, e := range bootstrap.GenerationCatalog() {
		svc.GenerationCatalog = append(svc.GenerationCatalog, opsapp.GenerationModelInfo{
			OutputType:   e.OutputType,
			Provider:     e.Provider,
			Models:       e.Models,
			DefaultModel: e.DefaultModel,
		})
	}
	return svc
}

// opsAWSClient surfaces the underlying *dynamodb.Client off the KV port so
// the ops service can run Scan operations the kv interface doesn't expose.
func opsAWSClient(a *bootstrap.AWS) (*dynamodb.Client, bool) {
	type clientHolder interface {
		Client() *dynamodb.Client
	}
	if h, ok := a.KV.(clientHolder); ok {
		return h.Client(), true
	}
	return nil, false
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
