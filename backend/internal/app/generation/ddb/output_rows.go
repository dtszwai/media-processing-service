package ddb

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func initialGenerationItem(job generation.Job, requestHash string, now time.Time) map[string]any {
	generationID := GenerationID(job.ID)
	outputID := OutputID(job.ID)
	variantCount := job.VariantCount
	if variantCount <= 0 {
		variantCount = 1
	}
	return map[string]any{
		"PK":                    JobPK(job.ID),
		"SK":                    GenerationSK(),
		"item_type":             "GENERATION",
		"tenant_id":             job.TenantID,
		"generation_id":         generationID,
		"active_job_id":         job.ID,
		"media_id":              job.MediaID,
		"created_by_user_id":    job.UserID,
		"output_type":           string(job.OutputType),
		"mode":                  string(generation.GenerationModeCreate),
		"status":                string(job.Status),
		"primary_output_id":     outputID,
		"request_hash":          requestHash,
		"pricing_version":       "v1",
		"safety_policy_version": "v1",
		"spec_summary": map[string]any{
			"output_type":   string(job.OutputType),
			"provider":      job.Provider,
			"model":         job.Model,
			"resolution":    job.Resolution,
			"seed":          job.Seed,
			"variant_count": variantCount,
			"tier":          string(job.Tier),
		},
		"created_at": now.Format(time.RFC3339Nano),
		"updated_at": now.Format(time.RFC3339Nano),
	}
}

func initialOutputItem(job generation.Job, now time.Time) map[string]any {
	outputID := OutputID(job.ID)
	variantCount := job.VariantCount
	if variantCount <= 0 {
		variantCount = 1
	}
	return map[string]any{
		"PK":                      JobPK(job.ID),
		"SK":                      OutputSK(outputID),
		"item_type":               "OUTPUT",
		"tenant_id":               job.TenantID,
		"output_id":               outputID,
		"generation_id":           GenerationID(job.ID),
		"job_id":                  job.ID,
		"media_id":                job.MediaID,
		"type":                    string(job.OutputType),
		"status":                  string(job.Status),
		"variant_count_requested": variantCount,
		"variant_count_completed": 0,
		"default_variant_id":      "",
		"created_at":              now.Format(time.RFC3339Nano),
		"updated_at":              now.Format(time.RFC3339Nano),
	}
}

func finalVariantItem(job generation.Job, assetID string, art generation.Artifact, now time.Time) map[string]any {
	outputID := OutputID(job.ID)
	variantID := VariantID(job.ID, 0)
	return map[string]any{
		"PK":                           JobPK(job.ID),
		"SK":                           VariantSK(variantID),
		"item_type":                    "VARIANT",
		"tenant_id":                    job.TenantID,
		"variant_id":                   variantID,
		"output_id":                    outputID,
		"generation_id":                GenerationID(job.ID),
		"job_id":                       job.ID,
		"media_id":                     job.MediaID,
		"index":                        0,
		"status":                       string(generation.StatusComplete),
		"final_asset_id":               assetID,
		"provider":                     art.Metadata["provider"],
		"model":                        firstNonEmpty(art.Metadata["model"], job.Model),
		"mime":                         art.ContentType,
		"bytes":                        int64(len(art.Bytes)),
		"provider_request_id":          job.ProviderRequestID,
		"watermark":                    map[string]any{"visible": art.Metadata["visible_watermark"], "fingerprint": art.Metadata["watermark.fingerprint"], "algorithm": art.Metadata["watermark.algo"]},
		"provenance_manifest_asset_id": "",
		"created_at":                   now.Format(time.RFC3339Nano),
		"updated_at":                   now.Format(time.RFC3339Nano),
		"completed_at":                 now.Format(time.RFC3339Nano),
	}
}

func completeGenerationOutputOps(job generation.Job, assetID string, art generation.Artifact, now time.Time) []kv.WriteOp {
	outputID := OutputID(job.ID)
	variantID := VariantID(job.ID, 0)
	return []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                finalVariantItem(job, assetID, art, now),
			ConditionExpression: "attribute_not_exists(PK) OR final_asset_id = :asset_id",
			ExpressionAttributeValues: kv.Values{
				":asset_id": assetID,
			},
		}},
		{Update: &kv.UpdateOp{
			Key:                 kv.Key{PK: JobPK(job.ID), SK: GenerationSK()},
			ConditionExpression: "attribute_exists(PK)",
			UpdateExpression:    "SET #st = :complete, completed_at = :now, updated_at = :now, primary_output_id = :output_id",
			ExpressionAttributeNames: kv.Names{
				"#st": "status",
			},
			ExpressionAttributeValues: kv.Values{
				":complete":  string(generation.StatusComplete),
				":now":       now.Format(time.RFC3339Nano),
				":output_id": outputID,
			},
		}},
		{Update: &kv.UpdateOp{
			Key:                 kv.Key{PK: JobPK(job.ID), SK: OutputSK(outputID)},
			ConditionExpression: "attribute_exists(PK)",
			UpdateExpression:    "SET #st = :complete, completed_at = :now, updated_at = :now, variant_count_completed = :one, default_variant_id = :variant_id",
			ExpressionAttributeNames: kv.Names{
				"#st": "status",
			},
			ExpressionAttributeValues: kv.Values{
				":complete":   string(generation.StatusComplete),
				":now":        now.Format(time.RFC3339Nano),
				":one":        1,
				":variant_id": variantID,
			},
		}},
	}
}

func failGenerationOutputOps(job generation.Job, terminalErr *generation.Error, now time.Time) []kv.WriteOp {
	errorCode := ""
	errorMessage := ""
	if terminalErr != nil {
		errorCode = terminalErr.Code
		errorMessage = terminalErr.Message
	}
	return terminalGenerationOutputOps(job, generation.StatusFailed, errorCode, errorMessage, now)
}

func cancelGenerationOutputOps(job generation.Job, reason string, now time.Time) []kv.WriteOp {
	return terminalGenerationOutputOps(job, generation.StatusCancelled, "CANCELLED", reason, now)
}

func terminalGenerationOutputOps(job generation.Job, status generation.Status, errorCode, errorMessage string, now time.Time) []kv.WriteOp {
	outputID := OutputID(job.ID)
	values := kv.Values{
		":status": string(status),
		":now":    now.Format(time.RFC3339Nano),
		":code":   errorCode,
		":msg":    errorMessage,
	}
	return []kv.WriteOp{
		{Update: &kv.UpdateOp{
			Key:                 kv.Key{PK: JobPK(job.ID), SK: GenerationSK()},
			ConditionExpression: "attribute_exists(PK)",
			UpdateExpression:    "SET #st = :status, completed_at = :now, updated_at = :now, error_code = :code, error_message = :msg",
			ExpressionAttributeNames: kv.Names{
				"#st": "status",
			},
			ExpressionAttributeValues: values,
		}},
		{Update: &kv.UpdateOp{
			Key:                 kv.Key{PK: JobPK(job.ID), SK: OutputSK(outputID)},
			ConditionExpression: "attribute_exists(PK)",
			UpdateExpression:    "SET #st = :status, completed_at = :now, updated_at = :now, error_code = :code, error_message = :msg",
			ExpressionAttributeNames: kv.Names{
				"#st": "status",
			},
			ExpressionAttributeValues: values,
		}},
	}
}

type outputRow struct {
	TenantID              string `dynamodbav:"tenant_id"`
	OutputID              string `dynamodbav:"output_id"`
	GenerationID          string `dynamodbav:"generation_id"`
	JobID                 string `dynamodbav:"job_id"`
	MediaID               string `dynamodbav:"media_id"`
	Type                  string `dynamodbav:"type"`
	Status                string `dynamodbav:"status"`
	VariantCountRequested int    `dynamodbav:"variant_count_requested"`
	VariantCountCompleted int    `dynamodbav:"variant_count_completed"`
	DefaultVariantID      string `dynamodbav:"default_variant_id"`
	CreatedAt             string `dynamodbav:"created_at"`
	UpdatedAt             string `dynamodbav:"updated_at"`
	CompletedAt           string `dynamodbav:"completed_at"`
}

type variantRow struct {
	TenantID                  string         `dynamodbav:"tenant_id"`
	VariantID                 string         `dynamodbav:"variant_id"`
	OutputID                  string         `dynamodbav:"output_id"`
	GenerationID              string         `dynamodbav:"generation_id"`
	JobID                     string         `dynamodbav:"job_id"`
	MediaID                   string         `dynamodbav:"media_id"`
	Index                     int            `dynamodbav:"index"`
	Status                    string         `dynamodbav:"status"`
	FinalAssetID              string         `dynamodbav:"final_asset_id"`
	StagedArtifactID          string         `dynamodbav:"staged_artifact_id"`
	Provider                  string         `dynamodbav:"provider"`
	Model                     string         `dynamodbav:"model"`
	Seed                      string         `dynamodbav:"seed"`
	ProviderRequestID         string         `dynamodbav:"provider_request_id"`
	SafetyCaseID              string         `dynamodbav:"safety_case_id"`
	Watermark                 map[string]any `dynamodbav:"watermark"`
	ProvenanceManifestAssetID string         `dynamodbav:"provenance_manifest_asset_id"`
	Score                     *float64       `dynamodbav:"score"`
	ErrorCode                 string         `dynamodbav:"error_code"`
	ErrorMessage              string         `dynamodbav:"error_message"`
	MIME                      string         `dynamodbav:"mime"`
	Bytes                     int64          `dynamodbav:"bytes"`
	CreatedAt                 string         `dynamodbav:"created_at"`
	UpdatedAt                 string         `dynamodbav:"updated_at"`
	CompletedAt               string         `dynamodbav:"completed_at"`
}

func (r *JobRepo) GetOutputRollup(ctx context.Context, tenantID, jobID string) (*generation.Output, []generation.Variant, error) {
	var outRow outputRow
	if err := r.KV.Get(ctx, kv.Key{PK: JobPK(jobID), SK: OutputSK(OutputID(jobID))}, &outRow); err != nil {
		return nil, nil, err
	}
	if tenantID != "" && outRow.TenantID != tenantID {
		return nil, nil, kv.ErrNotFound
	}
	out := outputRowToDomain(outRow)
	page, err := r.KV.Query(ctx, kv.QueryRequest{
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :sk)",
		ExpressionAttributeValues: kv.Values{
			":pk": JobPK(jobID),
			":sk": "VARIANT#",
		},
	})
	if err != nil {
		return nil, nil, err
	}
	variants := make([]generation.Variant, 0, len(page.Items))
	for _, item := range page.Items {
		var row variantRow
		if uerr := item.Unmarshal(&row); uerr != nil {
			return nil, nil, uerr
		}
		if tenantID != "" && row.TenantID != tenantID {
			continue
		}
		variants = append(variants, variantRowToDomain(row))
	}
	return &out, variants, nil
}

func outputRowToDomain(row outputRow) generation.Output {
	out := generation.Output{
		ID:                    row.OutputID,
		TenantID:              row.TenantID,
		MediaID:               row.MediaID,
		GenerationID:          row.GenerationID,
		JobID:                 row.JobID,
		Type:                  generation.OutputType(row.Type),
		Status:                generation.Status(row.Status),
		VariantCountRequested: row.VariantCountRequested,
		VariantCountCompleted: row.VariantCountCompleted,
		DefaultVariantID:      row.DefaultVariantID,
		CreatedAt:             parseTime(row.CreatedAt),
		UpdatedAt:             parseTime(row.UpdatedAt),
	}
	if t := parseTime(row.CompletedAt); !t.IsZero() {
		out.CompletedAt = &t
	}
	return out
}

func variantRowToDomain(row variantRow) generation.Variant {
	out := generation.Variant{
		ID:                        row.VariantID,
		TenantID:                  row.TenantID,
		MediaID:                   row.MediaID,
		OutputID:                  row.OutputID,
		GenerationID:              row.GenerationID,
		JobID:                     row.JobID,
		Index:                     row.Index,
		Status:                    generation.Status(row.Status),
		FinalAssetID:              row.FinalAssetID,
		StagedArtifactID:          row.StagedArtifactID,
		Provider:                  row.Provider,
		Model:                     row.Model,
		Seed:                      row.Seed,
		MIME:                      row.MIME,
		Bytes:                     uint64(row.Bytes),
		ProviderRequestID:         row.ProviderRequestID,
		SafetyCaseID:              row.SafetyCaseID,
		Watermark:                 row.Watermark,
		ProvenanceManifestAssetID: row.ProvenanceManifestAssetID,
		Score:                     row.Score,
		CreatedAt:                 parseTime(row.CreatedAt),
		UpdatedAt:                 parseTime(row.UpdatedAt),
	}
	if row.ErrorCode != "" || row.ErrorMessage != "" {
		out.Error = &generation.VariantError{Code: row.ErrorCode, Message: row.ErrorMessage}
	}
	if t := parseTime(row.CompletedAt); !t.IsZero() {
		out.CompletedAt = &t
	}
	return out
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
