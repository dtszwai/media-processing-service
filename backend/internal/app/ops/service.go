// Package ops powers the operator console at frontend/apps/web. It is a
// LOCAL_ONLY surface — the API gates the OpsService handler behind the same
// env flag — so production wiring MUST NOT mount it. The package reaches
// directly into infra (DynamoDB, S3, SQS, Loki) because the console's job is
// to surface raw infra state, not to translate it through the public-facing
// domain. Mutations write AUDIT#OPS#... rows so the audit invariant from
// AGENTS.md ("operator mutations are not exempt") is honoured.
package ops

import (
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	gendb "github.com/dtszwai/media-processing-service/backend/internal/app/generation/ddb"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// Service is the read + mutate surface the OpsService Connect handler
// delegates to. Fields are public so cmd wiring can swap individual ports in
// tests; production callers go through New.
type Service struct {
	KV     kv.KV
	DDB    *dynamodb.Client
	Blob   storage.Storage
	SQS    *sqs.Client
	Bucket string
	Table  string

	// JobRepo lends job lookup plus terminal mutations. The repo handles
	// prompt sealing, media-lifecycle flips, terminal rows, and rollups;
	// replicating those transitions in ops would diverge from generation.
	JobRepo *gendb.JobRepo

	// QueueURLs maps an operator-friendly queue name to its SQS URL.
	// Populated by cmd wiring from bootstrap.AWS.GenerationQueues +
	// MediaQueueURL + MediaUploadEventsQueueURL + WebhookQueueURL +
	// AnalyticsQueueURL.
	QueueURLs map[string]string
	// DLQs maps DLQ name -> (URL, source queue URL). Source URLs let
	// RedriveDlq pump messages back to their origin.
	DLQs map[string]DLQInfo

	// Loki is the log-source HTTP client. Nil in tests.
	Loki *LokiClient

	// LocalUserID / LocalTenantID identify the operator action's actor on
	// AUDIT#OPS rows. The console is single-tenant local-only; these are
	// hard-coded by cmd wiring (the auth-stub identity).
	LocalUserID   string
	LocalTenantID string
	// TenantCostCapMicroUSD is the configured default used before the
	// tenant's daily COST_MICRO_USD reservoir has been materialized.
	TenantCostCapMicroUSD int64

	// Logger for non-fatal diagnostic emit during mutations.
	Logger *slog.Logger

	// Clock is the time source. Defaults to time.Now().UTC() in New.
	Clock func() time.Time

	// GenerationCatalog is the per-output-type provider + model list the
	// submit form reads on mount. Populated by cmd wiring from the resolved
	// app.GenerationConfig; an empty slice means the catalog endpoint
	// returns no providers (the form falls back to a "default" placeholder).
	GenerationCatalog []GenerationModelInfo
}

// GenerationModelInfo is the catalog row the submit form renders. Mirrors
// bootstrap.GenerationModelInfo at the app layer to avoid importing
// bootstrap (which pulls every provider driver into the build).
type GenerationModelInfo struct {
	OutputType   string
	Provider     string
	Models       []string
	DefaultModel string
}

// DLQInfo names a DLQ + its source queue. Same reason as GenerationModelInfo
// for not importing bootstrap.
type DLQInfo struct {
	Name      string
	URL       string
	SourceURL string
}

// New constructs a Service with sane defaults. Required fields (KV, SQS,
// Blob, JobRepo, Bucket) are passed positionally; optional fields (DLQs,
// Loki, QueueURLs) are populated with-style by the caller.
func New(k kv.KV, blob storage.Storage, sqsClient *sqs.Client, table, bucket string, jobRepo *gendb.JobRepo) *Service {
	return &Service{
		KV:        k,
		Blob:      blob,
		SQS:       sqsClient,
		Table:     table,
		Bucket:    bucket,
		JobRepo:   jobRepo,
		QueueURLs: map[string]string{},
		DLQs:      map[string]DLQInfo{},
		Clock:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) now() time.Time { return s.Clock() }
