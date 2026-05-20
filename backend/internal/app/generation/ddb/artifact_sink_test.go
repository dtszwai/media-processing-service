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
	if blob.lastPut.Key != "tenant-final/media-final/assets/asset-final.png" {
		t.Fatalf("storage key = %q, want tenant-final/media-final/assets/asset-final.png", blob.lastPut.Key)
	}
	if string(blob.lastBody) != "final-bytes" {
		t.Fatalf("stored body = %q, want final-bytes", string(blob.lastBody))
	}
	if blob.lastPut.ContentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", blob.lastPut.ContentType)
	}
	if blob.lastPut.Tags["tenant_id"] != "tenant-final" || blob.lastPut.Tags["media_id"] != "media-final" || blob.lastPut.Tags["asset_id"] != "asset-final" || blob.lastPut.Tags["origin"] != "generation" {
		t.Fatalf("storage tags = %#v, want tenant/media/asset generation tags", blob.lastPut.Tags)
	}
	if blob.lastPut.Metadata["disclosure"] != "AI_GENERATED_DISCLOSURE" || blob.lastPut.Metadata["visible_watermark"] != "wm" {
		t.Fatalf("storage metadata = %#v, want disclosure + watermark", blob.lastPut.Metadata)
	}
	if len(fkv.updates) != 3 {
		t.Fatalf("update ops = %d, want asset/generation/output updates", len(fkv.updates))
	}
	assetUpdate := fkv.updates[0]
	if assetUpdate.ExpressionAttributeValues[":key"] != "tenant-final/media-final/assets/asset-final.png" {
		t.Fatalf("asset storage key update = %v", assetUpdate.ExpressionAttributeValues[":key"])
	}
	if assetUpdate.ExpressionAttributeValues[":prov"].(map[string]any)["provider"] != "" || assetUpdate.ExpressionAttributeValues[":prov"].(map[string]any)["model"] != "model-v1" {
		t.Fatalf("asset provenance = %#v, want provider fallback model-v1", assetUpdate.ExpressionAttributeValues[":prov"])
	}

	if _, err := sink.StoreFinalArtifact(context.Background(), job, art); err != nil {
		t.Fatalf("retry StoreFinalArtifact: %v", err)
	}
	if blob.puts != 2 {
		t.Fatalf("storage puts = %d, want 2 retry attempts", blob.puts)
	}
}

type artifactSinkKV struct {
	rows    map[string]map[string]any
	updates []kv.UpdateOp
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
			if op.Update != nil {
				f.updates = append(f.updates, *op.Update)
			}
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
	puts     int
	lastPut  storage.PutInput
	lastBody []byte
}

func (s *artifactSinkStorage) Put(_ context.Context, in storage.PutInput) (storage.PutOutput, error) {
	s.puts++
	s.lastPut = in
	if in.Body != nil {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, in.Body)
		s.lastBody = buf.Bytes()
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
