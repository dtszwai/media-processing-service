package media

import (
	"context"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func (r *DDBRepo) PutImageMetadata(ctx context.Context, meta media.ImageMetadata) error {
	if meta.TenantID == "" || meta.MediaID == "" || meta.AssetID == "" {
		return fmt.Errorf("%w: image metadata: tenant_id, media_id, asset_id required", ErrInvalidInput)
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	meta.UpdatedAt = time.Now().UTC()
	return r.KV.Put(ctx, newImageMetadataRow(meta), kv.PutOptions{})
}
