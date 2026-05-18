package ddb

import (
	"context"
	"errors"
	"testing"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestPromptEnhancementStoreWritesEncryptedRowAndDedups(t *testing.T) {
	store := &promptEnhancementKV{rows: map[string]promptEnhancementRow{}}
	repo := &JobRepo{KV: store}
	rec := genapp.PromptEnhancementRecord{
		Ref:                 "enh_123",
		TenantID:            "tenant-1",
		JobID:               "job-1",
		OutputType:          generation.OutputImage,
		EncryptedPrompt:     []byte("sealed"),
		RawPromptHash:       "raw-hash",
		PolicyVersion:       "policy-v1",
		Provider:            "openai",
		Model:               "gpt-test",
		DownstreamProvider:  "codex",
		DownstreamModel:     "gpt-image",
		Resolution:          "1024x1024",
		VariantCount:        1,
		TokensIn:            10,
		TokensOut:           20,
		ServiceCostMicroUSD: 30,
		VendorRequestID:     "vendor-1",
		CreatedAt:           time.Unix(100, 0).UTC(),
		TTLEpoch:            time.Unix(100, 0).Add(time.Hour).Unix(),
	}
	if err := repo.PutPromptEnhancement(context.Background(), rec); err != nil {
		t.Fatalf("PutPromptEnhancement: %v", err)
	}
	if err := repo.PutPromptEnhancement(context.Background(), rec); err != nil {
		t.Fatalf("duplicate PutPromptEnhancement: %v", err)
	}
	got, err := repo.GetPromptEnhancement(context.Background(), rec.TenantID, rec.JobID, rec.Ref)
	if err != nil {
		t.Fatalf("GetPromptEnhancement: %v", err)
	}
	if got.Ref != rec.Ref || got.RawPromptHash != rec.RawPromptHash || string(got.EncryptedPrompt) != "sealed" {
		t.Fatalf("record = %+v, want stored encrypted row", got)
	}
	if _, err := repo.GetPromptEnhancement(context.Background(), "other-tenant", rec.JobID, rec.Ref); !errors.Is(err, kv.ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v, want kv.ErrNotFound", err)
	}

	// Ref collision with a different raw_prompt_hash must surface as a hard
	// error rather than silently shadowing the stored row.
	collision := rec
	collision.RawPromptHash = "different-hash"
	collision.EncryptedPrompt = []byte("other")
	if err := repo.PutPromptEnhancement(context.Background(), collision); err == nil {
		t.Fatal("expected ref-collision error when raw_prompt_hash differs")
	}
}

type promptEnhancementKV struct {
	rows map[string]promptEnhancementRow
}

func (s *promptEnhancementKV) Put(_ context.Context, item kv.Item, _ kv.PutOptions) error {
	row, ok := item.(promptEnhancementRow)
	if !ok {
		return errors.New("promptEnhancementKV: Put requires promptEnhancementRow")
	}
	key := row.PK + "\x00" + row.SK
	if _, exists := s.rows[key]; exists {
		return kv.ErrConditionFailed
	}
	s.rows[key] = row
	return nil
}

func (s *promptEnhancementKV) Get(_ context.Context, key kv.Key, out any) error {
	row, ok := s.rows[key.PK+"\x00"+key.SK]
	if !ok {
		return kv.ErrNotFound
	}
	dst, ok := out.(*promptEnhancementRow)
	if !ok {
		return errors.New("promptEnhancementKV: Get requires *promptEnhancementRow")
	}
	*dst = row
	return nil
}

func (s *promptEnhancementKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("promptEnhancementKV: Query unsupported")
}

func (s *promptEnhancementKV) Update(context.Context, kv.UpdateOp) error {
	return errors.New("promptEnhancementKV: Update unsupported")
}

func (s *promptEnhancementKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("promptEnhancementKV: UpdateReturning unsupported")
}

func (s *promptEnhancementKV) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("promptEnhancementKV: Delete unsupported")
}

func (s *promptEnhancementKV) TransactWrite(context.Context, []kv.WriteOp) error {
	return errors.New("promptEnhancementKV: TransactWrite unsupported")
}
