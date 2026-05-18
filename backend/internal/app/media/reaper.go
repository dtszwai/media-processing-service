package media

import (
	"context"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

// Reaper finds stale PENDING media rows (older than MaxAge) via a
// gsi_lifecycle Query and flips them to FAILED in one transaction along with a
// cleanup outbox row. The cleanup worker tolerates S3 404 as success since
// init may have been abandoned before the client uploaded anything.
type Reaper struct {
	KV     kv.KV
	Repo   Repository
	MaxAge time.Duration
	Now    func() time.Time
}

// NewReaper binds the reaper to kv + repo.
func NewReaper(k kv.KV, repo Repository) *Reaper {
	return &Reaper{
		KV: k, Repo: repo,
		MaxAge: 24 * time.Hour,
		Now:    func() time.Time { return time.Now().UTC() },
	}
}

// Run queries gsi_lifecycle for stale PENDING rows and fails them.
// Returns the count of rows flipped to FAILED.
func (r *Reaper) Run(ctx context.Context, tenantID string) (int, error) {
	if r.KV == nil || r.Repo == nil {
		return 0, errors.New("reaper: kv + repo required")
	}
	cutoff := r.Now().Add(-r.MaxAge).Format(time.RFC3339Nano)
	pk := "TENANT#" + tenantID + "#LIFECYCLE#" + string(media.LifecyclePending)

	var startKey *kv.Key
	flipped := 0
	for {
		page, err := r.KV.Query(ctx, kv.QueryRequest{
			Index:                  "gsi_lifecycle",
			KeyConditionExpression: "gsi_lifecycle_pk = :pk AND gsi_lifecycle_sk < :cutoff",
			ExpressionAttributeValues: kv.Values{
				":pk":     pk,
				":cutoff": cutoff,
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return flipped, err
		}
		for _, row := range page.Items {
			itemTenantID, _ := row.Get("tenant_id").(string)
			mediaID, _ := row.Get("id").(string)
			assetID, _ := row.Get("original_asset_id").(string)
			if itemTenantID == "" || mediaID == "" || assetID == "" {
				continue
			}
			asset, err := r.Repo.GetAsset(ctx, itemTenantID, mediaID, assetID)
			if err != nil {
				return flipped, err
			}
			if asset.StorageKey == "" {
				return flipped, errors.New("reaper: pending upload asset missing storage_key")
			}
			cleanup := outbox.Row{
				Stream:      outbox.StreamMediaCleanup,
				PartitionTS: r.Now(),
				Shard:       shardkey.Of(mediaID, 8),
				EventID:     "reap-" + mediaID,
				Body:        buildCleanupOutboxBody(itemTenantID, mediaID, assetID, asset.StorageKey, "STALE_PENDING"),
				EventType:   string(events.EventMediaFailed),
				TenantID:    itemTenantID,
				Reason:      "STALE_PENDING",
			}
			if err := r.Repo.FailPresignedUpload(ctx, itemTenantID, mediaID, assetID, cleanup, r.Now()); err != nil {
				return flipped, err
			}
			flipped++
		}
		if page.LastEvaluatedKey == nil {
			break
		}
		startKey = page.LastEvaluatedKey
	}
	return flipped, nil
}

