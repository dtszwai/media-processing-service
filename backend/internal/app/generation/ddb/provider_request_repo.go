package ddb

import (
	"context"
	"errors"
	"strings"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func (r *JobRepo) PutProviderRequest(ctx context.Context, req genapp.ProviderRequest) error {
	if req.JobID == "" || req.ID == "" || req.TenantID == "" {
		return errors.New("provider request: tenant + job + request id required")
	}
	now := req.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if req.UpdatedAt.IsZero() {
		req.UpdatedAt = now
	}
	item := map[string]any{
		"PK":                      JobPK(req.JobID),
		"SK":                      ProviderRequestSK(req.ID),
		"item_type":               "PROVIDER_REQUEST",
		"tenant_id":               req.TenantID,
		"job_id":                  req.JobID,
		"provider_request_id":     req.ID,
		"provider":                req.Provider,
		"model":                   req.Model,
		"call_type":               req.CallType,
		"request_hash":            req.RequestHash,
		"vendor_request_id":       req.VendorRequestID,
		"vendor_idempotency_mode": string(req.VendorIdempotencyMode),
		"status":                  string(req.Status),
		"provider_job_id":         req.ProviderJobID,
		"created_at":              now.Format(time.RFC3339Nano),
		"updated_at":              req.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"ttl_epoch":               now.Add(365 * 24 * time.Hour).Unix(),
	}
	return r.KV.Put(ctx, item, kv.PutOptions{ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)"})
}

func (r *JobRepo) UpdateProviderRequest(ctx context.Context, tenantID, jobID, requestID string, status genapp.ProviderRequestStatus, providerJobID string, reqErr error) error {
	if jobID == "" || requestID == "" {
		return errors.New("provider request update: job + request id required")
	}
	now := time.Now().UTC()
	vals := kv.Values{
		":status": string(status),
		":now":    now.Format(time.RFC3339Nano),
	}
	sets := []string{"#st = :status", "updated_at = :now"}
	names := kv.Names{"#st": "status"}
	if tenantID != "" {
		vals[":tenant"] = tenantID
	}
	if providerJobID != "" {
		vals[":pjid"] = providerJobID
		sets = append(sets, "provider_job_id = :pjid")
	}
	if reqErr != nil {
		vals[":ec"] = generation.AsError(reqErr).Code
		vals[":em"] = reqErr.Error()
		sets = append(sets, "error_code = :ec", "error_message = :em")
	}
	if status == genapp.ProviderRequestSucceeded || status == genapp.ProviderRequestFailed {
		vals[":done"] = now.Format(time.RFC3339Nano)
		sets = append(sets, "completed_at = :done")
	}
	cond := "attribute_exists(PK)"
	if tenantID != "" {
		cond += " AND tenant_id = :tenant"
	}
	return r.KV.Update(ctx, kv.UpdateOp{
		Key:                       kv.Key{PK: JobPK(jobID), SK: ProviderRequestSK(requestID)},
		ConditionExpression:       cond,
		UpdateExpression:          "SET " + strings.Join(sets, ", "),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: vals,
	})
}
