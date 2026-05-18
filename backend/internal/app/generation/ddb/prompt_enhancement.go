package ddb

import (
	"context"
	"errors"
	"fmt"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

type promptEnhancementRow struct {
	PK                  string    `dynamodbav:"PK"`
	SK                  string    `dynamodbav:"SK"`
	ItemType            string    `dynamodbav:"item_type"`
	TenantID            string    `dynamodbav:"tenant_id"`
	JobID               string    `dynamodbav:"job_id"`
	Ref                 string    `dynamodbav:"ref"`
	OutputType          string    `dynamodbav:"output_type"`
	EncryptedPrompt     []byte    `dynamodbav:"encrypted_prompt"`
	RawPromptHash       string    `dynamodbav:"raw_prompt_hash"`
	PolicyVersion       string    `dynamodbav:"policy_version"`
	Provider            string    `dynamodbav:"provider"`
	Model               string    `dynamodbav:"model,omitempty"`
	DownstreamProvider  string    `dynamodbav:"downstream_provider,omitempty"`
	DownstreamModel     string    `dynamodbav:"downstream_model,omitempty"`
	Resolution          string    `dynamodbav:"resolution,omitempty"`
	VariantCount        int       `dynamodbav:"variant_count,omitempty"`
	TokensIn            int64     `dynamodbav:"tokens_in,omitempty"`
	TokensOut           int64     `dynamodbav:"tokens_out,omitempty"`
	ServiceCostMicroUSD int64     `dynamodbav:"service_cost_micro_usd,omitempty"`
	VendorRequestID     string    `dynamodbav:"vendor_request_id,omitempty"`
	CreatedAt           time.Time `dynamodbav:"created_at"`
	TTLEpoch            int64     `dynamodbav:"ttl_epoch"`
}

func (r *JobRepo) PutPromptEnhancement(ctx context.Context, rec genapp.PromptEnhancementRecord) error {
	err := r.KV.Put(ctx, promptEnhancementRowFromRecord(rec), kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return err
	}
	// A row already exists at this (job, ref). Confirm it represents the
	// same input — different raw_prompt_hash at the same ref means the ref
	// derivation collided across inputs, which is a hard bug we must
	// surface rather than silently shadow.
	existing, getErr := r.GetPromptEnhancement(ctx, rec.TenantID, rec.JobID, rec.Ref)
	if getErr != nil {
		return fmt.Errorf("prompt enhancement: verify existing on conflict: %w", getErr)
	}
	if existing.RawPromptHash != rec.RawPromptHash {
		return fmt.Errorf("prompt enhancement: ref %q conflict — stored raw_prompt_hash %q != incoming %q", rec.Ref, existing.RawPromptHash, rec.RawPromptHash)
	}
	return nil
}

func (r *JobRepo) GetPromptEnhancement(ctx context.Context, tenantID, jobID, ref string) (genapp.PromptEnhancementRecord, error) {
	var row promptEnhancementRow
	if err := r.KV.Get(ctx, kv.Key{PK: JobPK(jobID), SK: PromptEnhancementSK(ref)}, &row); err != nil {
		return genapp.PromptEnhancementRecord{}, err
	}
	if tenantID != "" && row.TenantID != tenantID {
		return genapp.PromptEnhancementRecord{}, kv.ErrNotFound
	}
	return row.toRecord(), nil
}

func promptEnhancementRowFromRecord(rec genapp.PromptEnhancementRecord) promptEnhancementRow {
	return promptEnhancementRow{
		PK:                  JobPK(rec.JobID),
		SK:                  PromptEnhancementSK(rec.Ref),
		ItemType:            "PROMPT_ENHANCEMENT",
		TenantID:            rec.TenantID,
		JobID:               rec.JobID,
		Ref:                 rec.Ref,
		OutputType:          string(rec.OutputType),
		EncryptedPrompt:     rec.EncryptedPrompt,
		RawPromptHash:       rec.RawPromptHash,
		PolicyVersion:       rec.PolicyVersion,
		Provider:            rec.Provider,
		Model:               rec.Model,
		DownstreamProvider:  rec.DownstreamProvider,
		DownstreamModel:     rec.DownstreamModel,
		Resolution:          rec.Resolution,
		VariantCount:        rec.VariantCount,
		TokensIn:            rec.TokensIn,
		TokensOut:           rec.TokensOut,
		ServiceCostMicroUSD: rec.ServiceCostMicroUSD,
		VendorRequestID:     rec.VendorRequestID,
		CreatedAt:           rec.CreatedAt,
		TTLEpoch:            rec.TTLEpoch,
	}
}

func (r promptEnhancementRow) toRecord() genapp.PromptEnhancementRecord {
	return genapp.PromptEnhancementRecord{
		Ref:                 r.Ref,
		TenantID:            r.TenantID,
		JobID:               r.JobID,
		OutputType:          generation.OutputType(r.OutputType),
		EncryptedPrompt:     r.EncryptedPrompt,
		RawPromptHash:       r.RawPromptHash,
		PolicyVersion:       r.PolicyVersion,
		Provider:            r.Provider,
		Model:               r.Model,
		DownstreamProvider:  r.DownstreamProvider,
		DownstreamModel:     r.DownstreamModel,
		Resolution:          r.Resolution,
		VariantCount:        r.VariantCount,
		TokensIn:            r.TokensIn,
		TokensOut:           r.TokensOut,
		ServiceCostMicroUSD: r.ServiceCostMicroUSD,
		VendorRequestID:     r.VendorRequestID,
		CreatedAt:           r.CreatedAt,
		TTLEpoch:            r.TTLEpoch,
	}
}
