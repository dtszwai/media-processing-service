package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func (r *DDBRepo) PutAsset(ctx context.Context, a media.Asset) error {
	row, err := newAssetRow(a)
	if err != nil {
		return err
	}
	return r.KV.Put(ctx, row, kv.PutOptions{})
}

// PutAssetIfAbsent creates the asset row only when no row exists at that key.
// Returns inserted=false when the deterministic asset already exists.
func (r *DDBRepo) PutAssetIfAbsent(ctx context.Context, a media.Asset) (bool, error) {
	row, err := newAssetRow(a)
	if err != nil {
		return false, err
	}
	err = r.KV.Put(ctx, row, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, kv.ErrConditionFailed) {
		return false, nil
	}
	return false, err
}

func (r *DDBRepo) GetAsset(ctx context.Context, tenantID, mediaID, assetID string) (*media.Asset, error) {
	var row assetRow
	if err := r.KV.Get(ctx, AssetKey(tenantID, mediaID, assetID), &row); err != nil {
		return nil, err
	}
	out := row.toDomain()
	return &out, nil
}

// FindByRole queries gsi_asset_role for the highest-priority COMPLETE asset
// matching the requested role. When opts.AcceptFallback is set and the exact
// role partition is empty, the lookup retries against the ORIGINAL partition
// so /download-url can still serve the source bytes for media types that
// haven't produced a role-specific derivative yet.
//
// The exact-role probe filters in DDB rather than client-side because a stale
// PENDING / FAILED asset would otherwise shadow a COMPLETE one with the same
// priority bucket.
func (r *DDBRepo) FindByRole(ctx context.Context, tenantID, mediaID string, role media.AssetRole, opts FindByRoleOpts) (*media.Asset, error) {
	if a, err := r.queryRolePartition(ctx, tenantID, mediaID, role); err != nil {
		return nil, err
	} else if a != nil {
		return a, nil
	}
	if opts.AcceptFallback && role != media.AssetRoleOriginal {
		if a, err := r.queryRolePartition(ctx, tenantID, mediaID, media.AssetRoleOriginal); err != nil {
			return nil, err
		} else if a != nil {
			return a, nil
		}
	}
	return nil, ErrNoAssetForRole
}

func (r *DDBRepo) queryRolePartition(ctx context.Context, tenantID, mediaID string, role media.AssetRole) (*media.Asset, error) {
	// Page through the role partition rather than relying on Limit=1: the
	// FilterExpression executes after Limit on the server, so a single stale
	// PENDING / FAILED row at the front of the partition would otherwise
	// produce a false negative when a COMPLETE row exists right behind it.
	var startKey *kv.Key
	for {
		page, err := r.KV.Query(ctx, kv.QueryRequest{
			Index:                  "gsi_asset_role",
			KeyConditionExpression: "gsi_asset_role_pk = :pk",
			FilterExpression:       "lifecycle = :complete",
			ExpressionAttributeValues: kv.Values{
				":pk":       RoleGSIPK(tenantID, mediaID, role),
				":complete": string(media.AssetLifecycleComplete),
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			var ar assetRow
			if uerr := item.Unmarshal(&ar); uerr != nil {
				return nil, uerr
			}
			a := ar.toDomain()
			return &a, nil
		}
		if page.LastEvaluatedKey == nil {
			return nil, nil
		}
		startKey = page.LastEvaluatedKey
	}
}

func (r *DDBRepo) ListAssets(ctx context.Context, tenantID, mediaID string) ([]media.Asset, error) {
	page, err := r.KV.Query(ctx, kv.QueryRequest{
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :sk)",
		ExpressionAttributeValues: kv.Values{
			":pk": MediaPK(tenantID, mediaID),
			":sk": "ASSET#",
		},
	})
	if err != nil {
		return nil, err
	}
	out := make([]media.Asset, 0, len(page.Items))
	for _, item := range page.Items {
		var ar assetRow
		if uerr := item.Unmarshal(&ar); uerr != nil {
			return nil, uerr
		}
		out = append(out, ar.toDomain())
	}
	return out, nil
}

// MarkAssetDeleted flips an asset row's lifecycle to DELETED conditional on
// "not already DELETED" so cleanup-worker retries are no-ops. The
// `ttl_epoch = now + AssetSoftDeleteRetention` matches the parent Media row's
// soft-delete retention so analytics keeps a uniform window for tombstoned
// rows before DynamoDB's TTL sweep removes them.
func (r *DDBRepo) MarkAssetDeleted(ctx context.Context, tenantID, mediaID, assetID string, now time.Time) error {
	err := r.KV.Update(ctx, kv.UpdateOp{
		Key:                 AssetKey(tenantID, mediaID, assetID),
		ConditionExpression: "lifecycle <> :deleted",
		UpdateExpression:    "SET lifecycle = :deleted, deleted_at = :now, ttl_epoch = :ttl, updated_at = :now",
		ExpressionAttributeValues: kv.Values{
			":deleted": string(media.AssetLifecycleDeleted),
			":now":     now.Format(time.RFC3339Nano),
			":ttl":     now.Add(SoftDeleteRetention).Unix(),
		},
	})
	if errors.Is(err, kv.ErrConditionFailed) {
		return nil
	}
	return err
}

// RetryAsset flips a FAILED asset to PROCESSING (attempts++) and stages a
// media.v1.process outbox row in one TransactWrite. The condition
// (lifecycle = FAILED AND attempts < maxAttempts) prevents concurrent retries
// from double-incrementing and stops infinite redrive on deterministic failures.
//
// On ErrConditionFailed the asset is re-read to distinguish "not FAILED"
// from "retry budget exhausted", so the caller receives a precise error.
func (r *DDBRepo) RetryAsset(ctx context.Context, tenantID, mediaID, assetID string, maxAttempts uint32, row OutboxRow, now time.Time) (*media.Asset, error) {
	outboxOp := outbox.BuildPutOp(row)
	err := r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Update: &kv.UpdateOp{
			Key: AssetKey(tenantID, mediaID, assetID),
			// ADD attempts :one atomically increments the Number attribute even
			// when it doesn't exist yet (DDB treats missing numeric attributes as
			// zero for ADD).
			ConditionExpression: "lifecycle = :failed AND (attribute_not_exists(attempts) OR attempts < :max)",
			UpdateExpression:    "SET lifecycle = :processing, updated_at = :now ADD attempts :one",
			ExpressionAttributeValues: kv.Values{
				":failed":     string(media.AssetLifecycleFailed),
				":processing": string(media.AssetLifecycleProcessing),
				":now":        now.Format(time.RFC3339Nano),
				":max":        maxAttempts,
				":one":        uint32(1),
			},
		}},
		{Put: &outboxOp},
	})
	if err == nil {
		return r.GetAsset(ctx, tenantID, mediaID, assetID)
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return nil, err
	}

	a, getErr := r.GetAsset(ctx, tenantID, mediaID, assetID)
	if getErr != nil {
		return nil, getErr
	}
	if a.Lifecycle != media.AssetLifecycleFailed {
		return nil, fmt.Errorf("%w: asset is not in FAILED state (lifecycle=%s)", ErrPreconditionFailed, a.Lifecycle)
	}
	return nil, ErrRetryExhausted
}
