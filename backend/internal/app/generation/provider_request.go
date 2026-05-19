package generation

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

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

// vendorRequestID is the stable per-job vendor idempotency key — a retry of
// the same job replays the same key so adapters with
// ProviderIdempotencySupported dedupe against it.
func vendorRequestID(job *generation.Job, provider string) string {
	return "vr_" + idempotency.HashInputs(job.TenantID, job.ID, provider, job.PreparedPromptHash, job.GenerationParamsHash)[:24]
}

func (w *Workflow) providerRequest(job *generation.Job, provider, callType, requestHash, vendorRequestID string) ProviderRequest {
	now := w.now()
	id := "pr_" + idempotency.HashInputs(job.ID, provider, callType, requestHash, now.Format(time.RFC3339Nano))[:24]
	return ProviderRequest{
		ID:                    id,
		TenantID:              job.TenantID,
		JobID:                 job.ID,
		Provider:              provider,
		Model:                 job.Model,
		CallType:              callType,
		RequestHash:           requestHash,
		VendorRequestID:       vendorRequestID,
		VendorIdempotencyMode: genprovider.VendorIdempotency(w.Provider),
		Status:                ProviderRequestSubmitted,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func (w *Workflow) putProviderRequest(ctx context.Context, req ProviderRequest) error {
	if w.ProviderRequests == nil {
		return nil
	}
	return w.ProviderRequests.PutProviderRequest(ctx, req)
}

func (w *Workflow) updateProviderRequest(ctx context.Context, job *generation.Job, requestID string, status ProviderRequestStatus, providerJobID string, err error) error {
	if w.ProviderRequests == nil {
		return nil
	}
	return w.ProviderRequests.UpdateProviderRequest(ctx, job.TenantID, job.ID, requestID, status, providerJobID, err)
}
