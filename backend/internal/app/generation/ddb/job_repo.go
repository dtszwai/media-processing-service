package ddb

import (
	"context"
	"errors"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// JobRepo persists Job state on the single table. Stage transitions write
// atomically alongside the next-stage outbox row, optional terminal Media
// lifecycle flip, optional gate audit row, and optional budget ledger ops.
type JobRepo struct {
	KV          kv.KV
	Sealer      genapp.PromptSealer
	QuotaLedger genapp.QuotaLedger
}

// NewJobRepo binds the impl to a kv driver and the optional prompt sealer
// used when reading/writing job rows. Pass nil for the sealer in tests.
func NewJobRepo(k kv.KV, sealer genapp.PromptSealer) *JobRepo {
	return &JobRepo{KV: k, Sealer: sealer}
}

type jobRow struct {
	PK                       string                `dynamodbav:"PK"`
	SK                       string                `dynamodbav:"SK"`
	ItemType                 string                `dynamodbav:"item_type"`
	GSIJobPK                 string                `dynamodbav:"gsi_job_pk"`
	GSIJobSK                 string                `dynamodbav:"gsi_job_sk"`
	ID                       string                `dynamodbav:"id"`
	TenantID                 string                `dynamodbav:"tenant_id"`
	UserID                   string                `dynamodbav:"user_id,omitempty"`
	MediaID                  string                `dynamodbav:"media_id,omitempty"`
	ResultAssetID            string                `dynamodbav:"result_asset_id,omitempty"`
	OutputType               generation.OutputType `dynamodbav:"output_type"`
	Tier                     generation.Tier       `dynamodbav:"tier"`
	Status                   generation.Status     `dynamodbav:"status"`
	CurrentStage             generation.Stage      `dynamodbav:"current_stage"`
	StageVersion             uint64                `dynamodbav:"stage_version"`
	Provider                 string                `dynamodbav:"provider,omitempty"`
	Model                    string                `dynamodbav:"model,omitempty"`
	Resolution               string                `dynamodbav:"resolution,omitempty"`
	Seed                     int64                 `dynamodbav:"seed,omitempty"`
	VariantCount             int                   `dynamodbav:"variant_count,omitempty"`
	PreparedPromptHash       string                `dynamodbav:"prepared_prompt_hash,omitempty"`
	PromptSpecVersion        string                `dynamodbav:"prompt_spec_version,omitempty"`
	PromptEnhancementApplied bool                  `dynamodbav:"prompt_enhancement_applied,omitempty"`
	PromptEnhancementRef     string                `dynamodbav:"prompt_enhancement_ref,omitempty"`
	GenerationParamsHash     string                `dynamodbav:"generation_parameters_hash,omitempty"`
	Attempts                 int                   `dynamodbav:"attempts,omitempty"`
	ProviderJobID            string                `dynamodbav:"provider_job_id,omitempty"`
	ProviderRequestID        string                `dynamodbav:"provider_request_id,omitempty"`
	BudgetDate               string                `dynamodbav:"budget_date,omitempty"`
	BudgetMicroUSD           int64                 `dynamodbav:"budget_micro_usd,omitempty"`
	CreatedAt                time.Time             `dynamodbav:"created_at"`
	UpdatedAt                time.Time             `dynamodbav:"updated_at"`
	CompletedAt              *time.Time            `dynamodbav:"completed_at,omitempty"`
	EncryptedPrompt          []byte                `dynamodbav:"encrypted_prompt,omitempty"`
	EncryptedPreparedPrompt  []byte                `dynamodbav:"encrypted_prepared_prompt,omitempty"`
	ErrorCode                string                `dynamodbav:"error_code,omitempty"`
	ErrorMessage             string                `dynamodbav:"error_message,omitempty"`
}

func (r *JobRepo) CreateJob(ctx context.Context, j generation.Job) error {
	if j.TenantID == "" || j.ID == "" {
		return errors.New("job: tenant + id required")
	}
	if j.CurrentStage == "" {
		j.CurrentStage = generation.StageInputModeration
	}
	if j.StageVersion == 0 {
		j.StageVersion = 1
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	j.UpdatedAt = j.CreatedAt
	row, err := r.row(ctx, j)
	if err != nil {
		return err
	}
	return r.KV.Put(ctx, row, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK)",
	})
}

func (r *JobRepo) row(ctx context.Context, j generation.Job) (jobRow, error) {
	if j.StageVersion == 0 {
		j.StageVersion = 1
	}
	plainPrompt := j.Prompt
	plainPrepared := j.PreparedPrompt
	var encPrompt, encPrepared []byte
	if plainPrompt != "" {
		if r.Sealer == nil {
			return jobRow{}, errors.New("job: prompt sealer required")
		}
		b, err := r.Sealer.Seal(ctx, j.TenantID, j.ID, plainPrompt)
		if err != nil {
			return jobRow{}, err
		}
		encPrompt = b
	}
	if plainPrepared != "" {
		if r.Sealer == nil {
			return jobRow{}, errors.New("job: prompt sealer required")
		}
		b, err := r.Sealer.Seal(ctx, j.TenantID, j.ID, plainPrepared)
		if err != nil {
			return jobRow{}, err
		}
		encPrepared = b
	}
	row := jobRowFromDomain(j)
	row.EncryptedPrompt = encPrompt
	row.EncryptedPreparedPrompt = encPrepared
	return row, nil
}

func jobRowFromDomain(j generation.Job) jobRow {
	if j.StageVersion == 0 {
		j.StageVersion = 1
	}
	return jobRow{
		PK:                       JobPK(j.ID),
		SK:                       JobSK,
		ItemType:                 "GEN",
		GSIJobPK:                 "TENANT#" + j.TenantID + "#STATUS#" + string(j.Status),
		GSIJobSK:                 j.CreatedAt.UTC().Format(time.RFC3339Nano) + "#" + j.ID,
		ID:                       j.ID,
		TenantID:                 j.TenantID,
		UserID:                   j.UserID,
		MediaID:                  j.MediaID,
		ResultAssetID:            j.ResultAssetID,
		OutputType:               j.OutputType,
		Tier:                     j.Tier,
		Status:                   j.Status,
		CurrentStage:             j.CurrentStage,
		StageVersion:             j.StageVersion,
		Provider:                 j.Provider,
		Model:                    j.Model,
		Resolution:               j.Resolution,
		Seed:                     j.Seed,
		VariantCount:             j.VariantCount,
		PreparedPromptHash:       j.PreparedPromptHash,
		PromptSpecVersion:        j.PromptSpecVersion,
		PromptEnhancementApplied: j.PromptEnhancementApplied,
		PromptEnhancementRef:     j.PromptEnhancementRef,
		GenerationParamsHash:     j.GenerationParamsHash,
		Attempts:                 j.Attempts,
		ProviderJobID:            j.ProviderJobID,
		ProviderRequestID:        j.ProviderRequestID,
		BudgetDate:               j.BudgetDate,
		BudgetMicroUSD:           j.BudgetMicroUSD,
		CreatedAt:                j.CreatedAt,
		UpdatedAt:                j.UpdatedAt,
		CompletedAt:              j.CompletedAt,
	}
}

func (r jobRow) toDomain() generation.Job {
	return generation.Job{
		ID:                       r.ID,
		TenantID:                 r.TenantID,
		UserID:                   r.UserID,
		MediaID:                  r.MediaID,
		ResultAssetID:            r.ResultAssetID,
		OutputType:               r.OutputType,
		Tier:                     r.Tier,
		Status:                   r.Status,
		CurrentStage:             r.CurrentStage,
		StageVersion:             r.StageVersion,
		Provider:                 r.Provider,
		Model:                    r.Model,
		Resolution:               r.Resolution,
		Seed:                     r.Seed,
		VariantCount:             r.VariantCount,
		PreparedPromptHash:       r.PreparedPromptHash,
		PromptSpecVersion:        r.PromptSpecVersion,
		PromptEnhancementApplied: r.PromptEnhancementApplied,
		PromptEnhancementRef:     r.PromptEnhancementRef,
		GenerationParamsHash:     r.GenerationParamsHash,
		Attempts:                 r.Attempts,
		ProviderJobID:            r.ProviderJobID,
		ProviderRequestID:        r.ProviderRequestID,
		BudgetDate:               r.BudgetDate,
		BudgetMicroUSD:           r.BudgetMicroUSD,
		CreatedAt:                r.CreatedAt,
		UpdatedAt:                r.UpdatedAt,
		CompletedAt:              r.CompletedAt,
	}
}

// GetJob reads the strong-consistent job row and unseals prompts.
func (r *JobRepo) GetJob(ctx context.Context, tenantID, jobID string) (*generation.Job, error) {
	var row jobRow
	if err := r.KV.Get(ctx, kv.Key{PK: JobPK(jobID), SK: JobSK}, &row); err != nil {
		return nil, err
	}
	out := row.toDomain()
	if tenantID != "" && out.TenantID != tenantID {
		return nil, kv.ErrNotFound
	}
	if len(row.EncryptedPrompt) > 0 {
		if r.Sealer == nil {
			return nil, errors.New("job.GetJob: prompt sealer required")
		}
		p, err := r.Sealer.Unseal(ctx, out.TenantID, out.ID, row.EncryptedPrompt)
		if err != nil {
			return nil, err
		}
		out.Prompt = p
	}
	if len(row.EncryptedPreparedPrompt) > 0 {
		if r.Sealer == nil {
			return nil, errors.New("job.GetJob: prompt sealer required")
		}
		p, err := r.Sealer.Unseal(ctx, out.TenantID, out.ID, row.EncryptedPreparedPrompt)
		if err != nil {
			return nil, err
		}
		out.PreparedPrompt = p
	}
	if row.ErrorCode != "" || row.ErrorMessage != "" {
		out.Error = &generation.Error{Code: row.ErrorCode, Message: row.ErrorMessage, Terminal: true}
	}
	return &out, nil
}
