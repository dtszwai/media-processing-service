package ddb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

func TestArtifactSinkSameAssetRetryDoesNotConflictOnVariantRow(t *testing.T) {
	fkv := &artifactSinkKV{rows: map[string]map[string]any{}}
	blob := &artifactSinkStorage{}
	sink := NewArtifactSink(fkv, blob)
	job := generation.Job{
		ID:            "gen_final_retry",
		TenantID:      "tenant-final",
		MediaID:       "media-final",
		ResultAssetID: "asset-final",
		OutputType:    generation.OutputImage,
		Model:         "model-v1",
	}
	art := generation.Artifact{
		Bytes:       []byte("final-bytes"),
		ContentType: "image/png",
		Extension:   "png",
		SHA256:      "sha-final",
		Metadata: map[string]string{
			"disclosure":            "AI_GENERATED_DISCLOSURE",
			"visible_watermark":     "wm",
			"watermark.fingerprint": "fp",
			"watermark.algo":        "test",
			"content_safety":        "safe",
		},
	}

	if _, err := sink.StoreFinalArtifact(context.Background(), job, art); err != nil {
		t.Fatalf("first StoreFinalArtifact: %v", err)
	}
	if _, err := sink.StoreFinalArtifact(context.Background(), job, art); err != nil {
		t.Fatalf("retry StoreFinalArtifact: %v", err)
	}
	if blob.puts != 2 {
		t.Fatalf("storage puts = %d, want 2 retry attempts", blob.puts)
	}
}

type artifactSinkKV struct {
	rows map[string]map[string]any
}

func (f *artifactSinkKV) Put(context.Context, kv.Item, kv.PutOptions) error {
	return errors.New("artifactSinkKV: Put not supported")
}

func (f *artifactSinkKV) Get(context.Context, kv.Key, any) error {
	return errors.New("artifactSinkKV: Get not supported")
}

func (f *artifactSinkKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("artifactSinkKV: Query not supported")
}

func (f *artifactSinkKV) Update(context.Context, kv.UpdateOp) error {
	return errors.New("artifactSinkKV: Update not supported")
}

func (f *artifactSinkKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("artifactSinkKV: UpdateReturning not supported")
}

func (f *artifactSinkKV) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("artifactSinkKV: Delete not supported")
}

func (f *artifactSinkKV) TransactWrite(_ context.Context, ops []kv.WriteOp) error {
	reasons := make([]kv.ItemCancelReason, len(ops))
	for i, op := range ops {
		if op.Put == nil {
			reasons[i] = kv.ItemCancelReason{Code: "None"}
			continue
		}
		row, ok := op.Put.Item.(map[string]any)
		if !ok {
			return errors.New("artifactSinkKV: Put item is not a map")
		}
		key := row["PK"].(string) + "\x00" + row["SK"].(string)
		existing, exists := f.rows[key]
		if exists && op.Put.ConditionExpression == "attribute_not_exists(PK) AND attribute_not_exists(SK)" {
			reasons[i] = kv.ItemCancelReason{ConditionFailed: true, Code: "ConditionalCheckFailed"}
			return &fakeTxnErr{items: reasons}
		}
		if exists && op.Put.ConditionExpression == "attribute_not_exists(PK) OR final_asset_id = :asset_id" && existing["final_asset_id"] != op.Put.ExpressionAttributeValues[":asset_id"] {
			reasons[i] = kv.ItemCancelReason{ConditionFailed: true, Code: "ConditionalCheckFailed"}
			return &fakeTxnErr{items: reasons}
		}
		f.rows[key] = cloneArtifactSinkRow(row)
		reasons[i] = kv.ItemCancelReason{Code: "None"}
	}
	return nil
}

func cloneArtifactSinkRow(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type artifactSinkStorage struct {
	puts int
}

func (s *artifactSinkStorage) Put(_ context.Context, in storage.PutInput) (storage.PutOutput, error) {
	s.puts++
	if in.Body != nil {
		_, _ = io.Copy(io.Discard, in.Body)
	}
	return storage.PutOutput{
		Key:       in.Key,
		SHA256Hex: in.SHA256Hex,
		SizeBytes: in.SizeBytes,
		ETag:      "etag-final",
	}, nil
}

func (s *artifactSinkStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *artifactSinkStorage) Delete(context.Context, string) error { return nil }

func (s *artifactSinkStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (s *artifactSinkStorage) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}

func (s *artifactSinkStorage) GetObjectAttributes(context.Context, string) (storage.ObjectAttrs, error) {
	return storage.ObjectAttrs{}, nil
}

func (s *artifactSinkStorage) HeadMetadata(context.Context, string) (map[string]string, error) {
	return nil, nil
}
