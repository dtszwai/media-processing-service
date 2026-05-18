// Package generation orchestrates the generation FSM.
package generation

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// SubmitInput is the cross-table submit payload: Media + result Asset + Job
// + idempotency claim + first-stage outbox row, persisted atomically by the
// JobRepository implementation. SNS attributes are not carried here — the
// outbox relay derives them from the persisted JobRow's semantic fields
// (tier, stage, resource_class) under a routing policy.
type SubmitInput struct {
	Job              generation.Job
	Media            media.Media
	ResultAsset      media.Asset
	IdempotencyScope string
	InputHash        string
	FirstStageBody   []byte
}

// JobRepository persists Job state. Reads use the tenant-aware lookup so
// stage messages stay ID-only and the base table remains the strong-consistent
// source of truth.
type JobRepository interface {
	CreateJob(ctx context.Context, j generation.Job) error
	GetJob(ctx context.Context, tenantID, jobID string) (*generation.Job, error)
	// AdvanceStageAndEnqueue atomically transitions a job to result.NextStage,
	// applies all stage-produced mutations, and writes the outbox row for the
	// next stage's SNS publish. Conditional on CurrentStage = previousStage.
	AdvanceStageAndEnqueue(ctx context.Context, job *generation.Job, result StageResult) error
}

type StageAttemptReader interface {
	LastStageAttempt(ctx context.Context, tenantID, jobID string, stage generation.Stage) (StageAttempt, error)
}

type StageAttempt struct {
	Stage        generation.Stage
	StageVersion uint64
	AttemptNo    int
	Result       string
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
}

// ArtifactSink persists the final artifact (S3 + Asset row update). It does
// NOT flip Media.Lifecycle to COMPLETE — that happens inside the terminal
// transition via AdvanceStageAndEnqueue so Media exposure stays atomic with
// the workflow's terminal commit.
type ArtifactSink interface {
	StoreFinalArtifact(ctx context.Context, j generation.Job, art generation.Artifact) (assetID string, err error)
}

// StageResult is what a stage handler returns. It carries the next stage plus
// every persisted mutation so AdvanceStageAndEnqueue can apply them in one
// TransactWriteItems together with the outbox row.
type StageResult struct {
	NextStage     generation.Stage
	OutboxBody    []byte
	ResourceClass generation.ResourceClass

	// Stage-produced mutations:
	BudgetDate              string
	BudgetMicroUSD          int64
	PreparedPrompt          string
	EncryptedPreparedPrompt []byte
	PreparedPromptHash      string
	PromptSpecVersion       string
	GenerationParamsHash    string
	ProviderRequestID       string
	ProviderJobID           string
	ResultAssetID           string
	AttemptsDelta           int
	TransientError          *generation.Error
	TerminalError           *generation.Error
	CompletedAt             *time.Time
	GateDecision            *GateDecision

	// LedgerOp, when non-nil, is appended to the TransactWriteItems slice in
	// AdvanceStageAndEnqueue so the per-job budget ledger row and its aggregate
	// counterpart transition atomically with the stage advance.
	LedgerOp *LedgerOp
}

func (r StageResult) IsTerminalComplete() bool {
	return r.NextStage == StageTerminal && r.TerminalError == nil && r.CompletedAt != nil
}

func (r StageResult) IsTerminalFailed() bool {
	return r.NextStage == StageTerminal && r.TerminalError != nil
}

// StageTerminal is the sentinel NextStage that marks a job terminal. Complete
// vs failed is carried by CompletedAt / TerminalError so the durable stage
// vocabulary has one terminal value: TERMINAL.
const StageTerminal = generation.StageTerminal

// LedgerOp is the per-reservation quota ledger operation that rides on the
// same TransactWrite as AdvanceStageAndEnqueue. Nil means no ledger item is
// appended (pre-reserve stages or terminal-failed with no charge).
//
// Items is contractually [aggregate UPDATE, ledger row PUT/UPDATE].
// AdvanceStageAndEnqueue attaches quota.OpAggregateTenantQuota and
// quota.OpLedgerTenantQuota to the two slots so cancellation classifies by
// name rather than slot position.
type LedgerOp struct {
	Items []kv.WriteOp
}

// QuotaLedger constructs per-reservation ledger TransactWriteItems. The
// workflow calls these helpers and threads the resulting LedgerOp into
// StageResult so AdvanceStageAndEnqueue can append them to its transaction
// slice without knowing DynamoDB key conventions. The reserved period is
// caller-controlled (yyyyMMdd for tenant cost) so the workflow stays
// scope-agnostic; the Reservoir primitive holds tenant cost today and is
// the same shape for API-key cost, vendor cost, request count, and storage
// bytes in follow-up slices.
type QuotaLedger interface {
	// LedgerPutReserved returns the txn item that inserts a RESERVED ledger row
	// plus the aggregate UPDATE that decrements available/increments reserved.
	// Called at COST_RESERVE stage success; both items ride with the stage
	// transition so the ledger is only RESERVED when the transition commits.
	LedgerPutReserved(tenantID, jobID, period string, amount int64, attempt int) LedgerOp

	// LedgerUpdateCommitted returns the txn item that flips the ledger row to
	// COMMITTED plus the aggregate UPDATE that moves reserved→committed.
	// Called at PROVIDER_SUBMIT success; both items ride with the post-inference
	// stage transition.
	LedgerUpdateCommitted(tenantID, jobID, period string, amount int64) LedgerOp

	// LedgerUpdateReleased returns the txn item that flips the ledger row to
	// RELEASED plus the aggregate UPDATE that returns reserved to available.
	// Called on terminal-failure before the provider charges; conditional on
	// state = RESERVED so a post-charge release cancels the transaction.
	LedgerUpdateReleased(tenantID, jobID, period string, amount int64) LedgerOp
}

type ProviderRequestStatus string

const (
	ProviderRequestSubmitted ProviderRequestStatus = "SUBMITTED"
	ProviderRequestSucceeded ProviderRequestStatus = "SUCCEEDED"
	ProviderRequestFailed    ProviderRequestStatus = "FAILED"
)

// ProviderIdempotencyMode is the persisted form of an adapter's vendor
// idempotency contract. Aliased to the vendor enum (identical string values)
// so the wire and storage shape stays one canonical type — provider adapters
// declare it once, the app/transport/repo layer reuses the same labels.
type ProviderIdempotencyMode = genprovider.VendorIdempotencyMode

const (
	ProviderIdempotencySupported   = genprovider.VendorIdempotencySupported
	ProviderIdempotencyBestEffort  = genprovider.VendorIdempotencyBestEffort
	ProviderIdempotencyUnsupported = genprovider.VendorIdempotencyUnsupported
)

type ProviderRequest struct {
	ID                    string
	TenantID              string
	JobID                 string
	Provider              string
	Model                 string
	CallType              string
	RequestHash           string
	VendorRequestID       string
	VendorIdempotencyMode ProviderIdempotencyMode
	Status                ProviderRequestStatus
	ProviderJobID         string
	ErrorCode             string
	ErrorMessage          string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

type ProviderRequestRepository interface {
	PutProviderRequest(ctx context.Context, req ProviderRequest) error
	UpdateProviderRequest(ctx context.Context, tenantID, jobID, requestID string, status ProviderRequestStatus, providerJobID string, err error) error
}
