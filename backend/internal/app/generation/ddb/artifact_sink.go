package ddb

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"time"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// ArtifactSink persists provider output to S3 and updates the already-allocated
// result Asset row. Does NOT flip Media — that ride on the terminal transition
// via AdvanceStageAndEnqueue so exposure stays atomic with the workflow commit.
type ArtifactSink struct {
	KV   kv.KV
	Blob storage.Storage
	Now  func() time.Time
}

// NewArtifactSink wires the sink to kv + storage drivers.
func NewArtifactSink(k kv.KV, blob storage.Storage) *ArtifactSink {
	return &ArtifactSink{KV: k, Blob: blob, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *ArtifactSink) StoreFinalArtifact(ctx context.Context, j generation.Job, art generation.Artifact) (string, error) {
	if s == nil || s.KV == nil || s.Blob == nil {
		return "", errors.New("generation artifact sink: kv + storage required")
	}
	if j.ResultAssetID == "" {
		return "", errors.New("generation artifact sink: result_asset_id required")
	}
	ext := cmp.Or(art.Extension, "bin")
	key := media.StorageKey(j.TenantID, j.MediaID, j.ResultAssetID, ext)
	put, err := s.Blob.Put(ctx, storage.PutInput{
		Key:         key,
		Body:        bytes.NewReader(art.Bytes),
		ContentType: art.ContentType,
		SizeBytes:   int64(len(art.Bytes)),
		SHA256Hex:   art.SHA256,
		Tags: map[string]string{
			"tenant_id": j.TenantID,
			"media_id":  j.MediaID,
			"asset_id":  j.ResultAssetID,
			"origin":    "generation",
		},
		Metadata: art.Metadata,
	})
	if err != nil {
		return "", err
	}

	prov := map[string]any{
		"provider":     art.Metadata["provider"],
		"model":        cmp.Or(art.Metadata["model"], j.Model),
		"ai_generated": true,
		"disclosure":   art.Metadata["disclosure"],
		"watermark":    art.Metadata["visible_watermark"],
		"safety":       art.Metadata["content_safety"],
	}
	now := s.Now()
	op := mediaapp.CompleteResultAssetOp(j.TenantID, j.MediaID, j.ResultAssetID, mediaapp.ResultArtifactRow{
		StorageKey:  put.Key,
		ContentType: art.ContentType,
		Extension:   ext,
		SizeBytes:   put.SizeBytes,
		SHA256Hex:   put.SHA256Hex,
		ETag:        put.ETag,
		Provenance:  prov,
	}, now)
	ops := append([]kv.WriteOp{{Update: &op}}, completeGenerationOutputOps(j, j.ResultAssetID, art, now)...)
	if err := s.KV.TransactWrite(ctx, ops); err != nil {
		return "", err
	}
	return j.ResultAssetID, nil
}
