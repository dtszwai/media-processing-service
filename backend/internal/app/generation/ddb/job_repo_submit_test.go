package ddb

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestJobRepoSubmitWritesGenerationEnvelopeAndRoutableFirstStage(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	firstStageBody, err := genapp.MarshalStageMessage("tenant-submit", "gen_submit", generation.StageInputModeration, 1, generation.ResourceFast, "00-trace")
	if err != nil {
		t.Fatalf("MarshalStageMessage: %v", err)
	}

	store := &submitCaptureKV{}
	repo := NewJobRepo(store, prefixSealer{})
	err = repo.Submit(ctx, genapp.SubmitInput{
		Job: generation.Job{
			ID:            "gen_submit",
			TenantID:      "tenant-submit",
			UserID:        "user-submit",
			MediaID:       "med_submit",
			ResultAssetID: "ast_submit",
			OutputType:    generation.OutputImage,
			Tier:          generation.TierPaid,
			Status:        generation.StatusQueued,
			CurrentStage:  generation.StageInputModeration,
			StageVersion:  1,
			Provider:      "codex",
			Model:         "gpt-5.5",
			Resolution:    "1024x1024",
			Seed:          99,
			VariantCount:  1,
			Prompt:        "raw prompt",
			CreatedAt:     now,
		},
		Media: media.Media{
			ID:              "med_submit",
			TenantID:        "tenant-submit",
			OwnerUserID:     "user-submit",
			Visibility:      media.DefaultVisibility("user-submit"),
			Origin:          media.OriginGenerated,
			Type:            media.TypeImage,
			Lifecycle:       media.LifecycleRunning,
			OriginalAssetID: "ast_submit",
			CreatedAt:       now,
		},
		ResultAsset: media.Asset{
			ID:        "ast_submit",
			MediaID:   "med_submit",
			TenantID:  "tenant-submit",
			Kind:      media.AssetKindGenerated,
			Role:      media.AssetRoleFinal,
			Operation: media.AssetOperationGenerationFinal,
			Lifecycle: media.AssetLifecyclePending,
			CreatedAt: now,
		},
		IdempotencyScope: "SUBMIT#tenant-submit#idem-submit",
		InputHash:        "submit-input-hash",
		FirstStageBody:   firstStageBody,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(store.ops) != 7 {
		t.Fatalf("transaction ops = %d, want media asset job generation output claim outbox", len(store.ops))
	}
	wantConditions := []string{
		"attribute_not_exists(PK)",
		"attribute_not_exists(SK)",
		"attribute_not_exists(PK) AND attribute_not_exists(SK)",
		"attribute_not_exists(PK) AND attribute_not_exists(SK)",
		"attribute_not_exists(PK) AND attribute_not_exists(SK)",
		"attribute_not_exists(PK)",
		"attribute_not_exists(PK)",
	}
	for i, want := range wantConditions {
		if store.ops[i].Put == nil {
			t.Fatalf("op %d is not a Put", i)
		}
		if got := store.ops[i].Put.ConditionExpression; got != want {
			t.Fatalf("op %d condition = %q, want %q", i, got, want)
		}
	}

	jobRow, ok := store.ops[2].Put.Item.(jobRow)
	if !ok {
		t.Fatalf("job op item type = %T, want jobRow", store.ops[2].Put.Item)
	}
	if string(jobRow.EncryptedPrompt) != "sealed:raw prompt" {
		t.Fatalf("job prompt was not sealed: %q", string(jobRow.EncryptedPrompt))
	}

	generationRow := store.ops[3].Put.Item.(map[string]any)
	if generationRow["PK"] != JobPK("gen_submit") || generationRow["SK"] != GenerationSK() {
		t.Fatalf("generation row key = %v/%v", generationRow["PK"], generationRow["SK"])
	}
	if generationRow["status"] != string(generation.StatusQueued) || generationRow["active_job_id"] != "gen_submit" {
		t.Fatalf("generation row status/active_job_id = %v/%v", generationRow["status"], generationRow["active_job_id"])
	}
	spec := generationRow["spec_summary"].(map[string]any)
	if spec["provider"] != "codex" || spec["model"] != "gpt-5.5" || spec["tier"] != string(generation.TierPaid) {
		t.Fatalf("spec summary = %#v, want codex/gpt-5.5/PAID", spec)
	}

	outputRow := store.ops[4].Put.Item.(map[string]any)
	if outputRow["status"] != string(generation.StatusQueued) || outputRow["variant_count_requested"] != 1 {
		t.Fatalf("output row status/variant_count = %v/%v", outputRow["status"], outputRow["variant_count_requested"])
	}

	claimRow := store.ops[5].Put.Item.(map[string]any)
	if claimRow["PK"] != persist.PK("SUBMIT#tenant-submit#idem-submit") || claimRow["status"] != string(idempotency.StatusCompleted) {
		t.Fatalf("claim row pk/status = %v/%v", claimRow["PK"], claimRow["status"])
	}
	if claimRow["input_hash"] != "submit-input-hash" || claimRow["result"] != "gen_submit:med_submit" {
		t.Fatalf("claim input_hash/result = %v/%v", claimRow["input_hash"], claimRow["result"])
	}

	outboxRow := store.ops[6].Put.Item.(map[string]any)
	if outboxRow["stream"] != outbox.StreamGeneration {
		t.Fatalf("outbox stream = %v, want %s", outboxRow["stream"], outbox.StreamGeneration)
	}
	if outboxRow["tenant_id"] != "tenant-submit" || outboxRow["tenant_lane"] != genapp.TenantLane("tenant-submit") {
		t.Fatalf("outbox tenant fields = %v/%v", outboxRow["tenant_id"], outboxRow["tenant_lane"])
	}
	if outboxRow["tier"] != string(generation.TierPaid) || outboxRow["stage"] != string(generation.StageInputModeration) || outboxRow["resource_class"] != string(generation.ResourceFast) {
		t.Fatalf("outbox routing fields = tier:%v stage:%v resource:%v", outboxRow["tier"], outboxRow["stage"], outboxRow["resource_class"])
	}
	if !bytes.Equal(outboxRow["body"].([]byte), firstStageBody) {
		t.Fatalf("outbox body mismatch")
	}
	msg, err := genapp.UnmarshalStageMessage(outboxRow["body"].([]byte))
	if err != nil {
		t.Fatalf("UnmarshalStageMessage: %v", err)
	}
	if msg.Stage != generation.StageInputModeration || msg.StageVersion != 1 || msg.ResourceClass != generation.ResourceFast {
		t.Fatalf("stage message = %+v, want INPUT_MODERATION v1 FAST", msg)
	}
	attrs, err := outbox.DefaultPolicy{}.AttributesFor(outbox.PendingRow{
		Stream:        outboxRow["stream"].(string),
		TenantLane:    outboxRow["tenant_lane"].(string),
		Tier:          outboxRow["tier"].(string),
		Stage:         outboxRow["stage"].(string),
		ResourceClass: outboxRow["resource_class"].(string),
	})
	if err != nil {
		t.Fatalf("routing policy rejected submit outbox row: %v", err)
	}
	if attrs["tier"] != string(generation.TierPaid) || attrs["resource_class"] != string(generation.ResourceFast) {
		t.Fatalf("routing attrs = %#v, want PAID/FAST", attrs)
	}
}

type submitCaptureKV struct {
	ops []kv.WriteOp
}

func (s *submitCaptureKV) Put(context.Context, kv.Item, kv.PutOptions) error {
	return errors.New("submitCaptureKV: Put unsupported")
}

func (s *submitCaptureKV) Get(context.Context, kv.Key, any) error {
	return errors.New("submitCaptureKV: Get unsupported")
}

func (s *submitCaptureKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("submitCaptureKV: Query unsupported")
}

func (s *submitCaptureKV) Update(context.Context, kv.UpdateOp) error {
	return errors.New("submitCaptureKV: Update unsupported")
}

func (s *submitCaptureKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("submitCaptureKV: UpdateReturning unsupported")
}

func (s *submitCaptureKV) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("submitCaptureKV: Delete unsupported")
}

func (s *submitCaptureKV) TransactWrite(_ context.Context, ops []kv.WriteOp) error {
	s.ops = append([]kv.WriteOp(nil), ops...)
	return nil
}
