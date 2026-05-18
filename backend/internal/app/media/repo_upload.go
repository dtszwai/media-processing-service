package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// InitPresignedUpload writes Media + Asset + idempotency claim atomically.
// On collision (claim row exists) replays the prior result.
func (r *DDBRepo) InitPresignedUpload(ctx context.Context, m media.Media, a media.Asset, scope, inputHash string, claimTTL time.Duration) (media.Media, media.Asset, error) {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.UpdatedAt = m.CreatedAt
	a.CreatedAt = m.CreatedAt
	a.UpdatedAt = m.CreatedAt

	mItem := newMediaRow(m)
	aItem, err := newAssetRow(a)
	if err != nil {
		return media.Media{}, media.Asset{}, err
	}
	claim := persist.NewCompletedClaim(scope, inputHash, m.ID+"/"+a.ID, m.CreatedAt, claimTTL,
		persist.WithMetadata(map[string]string{
			"tenant_id": m.TenantID,
			"media_id":  m.ID,
			"asset_id":  a.ID,
		}),
	)

	err = r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{Item: mItem, ConditionExpression: "attribute_not_exists(PK)"}},
		{Put: &kv.PutOp{Item: aItem, ConditionExpression: "attribute_not_exists(SK)"}},
		{Put: &kv.PutOp{Item: claim, ConditionExpression: "attribute_not_exists(PK)"}},
	})
	if err == nil {
		return m, a, nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return media.Media{}, media.Asset{}, err
	}
	rm, ra, rerr := r.replayInit(ctx, scope, inputHash)
	if rerr != nil {
		return media.Media{}, media.Asset{}, errors.Join(kv.ErrConditionFailed, rerr)
	}
	return rm, ra, nil
}

// CompletePresignedUpload adds the IDEMPOTENCY claim row to the completion
// transaction so the same scope cannot transition twice. Both completion
// paths (API HEAD + S3 ObjectCreated event) call this; the claim's
// attribute_not_exists guard is what collapses a race between them to one
// effective transition.
func (r *DDBRepo) CompletePresignedUpload(ctx context.Context, a media.Asset, mediaID, tenantID string, row outbox.Row, claimScope, claimInputHash string, claimTTL time.Duration, now time.Time) error {
	outboxOp := outbox.BuildPutOp(row)
	claim := persist.NewCompletedClaim(claimScope, claimInputHash, mediaID+"/"+a.ID, now, claimTTL)
	err := r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{Item: claim, ConditionExpression: "attribute_not_exists(PK)"}},
		{Update: r.completeAssetUpdate(a, mediaID, tenantID, now)},
		{Update: r.completeMediaUpdate(mediaID, tenantID, now)},
		{Put: &outboxOp},
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return err
	}
	// The transaction failed because either (a) the claim already exists (the
	// other completion path got here first) or (b) the lifecycle has already
	// moved out of PENDING. The claim scope encodes the storage version id,
	// so a colliding scope necessarily means the same input — the only thing
	// left to do is reconcile the post-transition row shape.
	return r.resolveCompleteRace(ctx, tenantID, mediaID, a.ID, err)
}

// completeAssetUpdate is the Asset row update shared by both completion
// transactions. Conditional on PENDING_UPLOAD so the second arrival to a
// completed row sees ErrConditionFailed and routes through the race resolver.
func (r *DDBRepo) completeAssetUpdate(a media.Asset, mediaID, tenantID string, now time.Time) *kv.UpdateOp {
	return &kv.UpdateOp{
		Key:                 AssetKey(tenantID, mediaID, a.ID),
		ConditionExpression: "lifecycle = :pending",
		UpdateExpression:    "SET lifecycle = :complete, size_bytes = :sz, content_type = :ct, etag = :etag, sha256 = :sha, updated_at = :now",
		ExpressionAttributeValues: kv.Values{
			":pending":  string(media.AssetLifecyclePendingUpload),
			":complete": string(media.AssetLifecycleComplete),
			":sz":       a.SizeBytes,
			":ct":       a.ContentType,
			":etag":     a.ETag,
			":sha":      a.SHA256,
			":now":      now.Format(time.RFC3339Nano),
		},
	}
}

// completeMediaUpdate is the Media row update shared by both completion
// transactions. Conditional on PENDING for the same reason.
func (r *DDBRepo) completeMediaUpdate(mediaID, tenantID string, now time.Time) *kv.UpdateOp {
	return &kv.UpdateOp{
		Key:                 MediaKey(tenantID, mediaID),
		ConditionExpression: "lifecycle = :pending",
		UpdateExpression:    "SET lifecycle = :processing, updated_at = :now, gsi_lifecycle_pk = :lc_pk, gsi_lifecycle_sk = :now",
		ExpressionAttributeValues: kv.Values{
			":pending":    string(media.LifecyclePending),
			":processing": string(media.LifecycleRunning),
			":now":        now.Format(time.RFC3339Nano),
			":lc_pk":      LifecycleGSIPK(tenantID, string(media.LifecycleRunning)),
		},
	}
}

// resolveCompleteRace reads the row state after a conditional-check failure.
// When the row is already in the post-transition shape both completion paths
// converge on, that's a successful replay (no-op for the caller). Anything
// else propagates the original error so the operator sees the real failure.
func (r *DDBRepo) resolveCompleteRace(ctx context.Context, tenantID, mediaID, assetID string, origErr error) error {
	currentAsset, getAssetErr := r.GetAsset(ctx, tenantID, mediaID, assetID)
	if getAssetErr != nil {
		return errors.Join(kv.ErrConditionFailed, getAssetErr)
	}
	currentMedia, getMediaErr := r.GetMedia(ctx, tenantID, mediaID)
	if getMediaErr != nil {
		return errors.Join(kv.ErrConditionFailed, getMediaErr)
	}
	if currentAsset.Lifecycle == media.AssetLifecycleComplete &&
		(currentMedia.Lifecycle == media.LifecycleRunning || currentMedia.Lifecycle == media.LifecycleComplete) {
		return nil
	}
	return origErr
}

// FailPresignedUpload flips Media + Asset to FAILED + writes the cleanup outbox row.
func (r *DDBRepo) FailPresignedUpload(ctx context.Context, tenantID, mediaID, assetID string, cleanup outbox.Row, now time.Time) error {
	outboxOp := outbox.BuildPutOp(cleanup)
	err := r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Update: &kv.UpdateOp{
			Key:                 AssetKey(tenantID, mediaID, assetID),
			ConditionExpression: "lifecycle = :pending",
			UpdateExpression:    "SET lifecycle = :failed, updated_at = :now",
			ExpressionAttributeValues: kv.Values{
				":pending": string(media.AssetLifecyclePendingUpload),
				":failed":  string(media.AssetLifecycleFailed),
				":now":     now.Format(time.RFC3339Nano),
			},
		}},
		{Update: &kv.UpdateOp{
			Key:                 MediaKey(tenantID, mediaID),
			ConditionExpression: "lifecycle = :pending",
			UpdateExpression:    "SET lifecycle = :failed, updated_at = :now, gsi_lifecycle_pk = :lc_pk, gsi_lifecycle_sk = :now",
			ExpressionAttributeValues: kv.Values{
				":pending": string(media.LifecyclePending),
				":failed":  string(media.LifecycleFailed),
				":now":     now.Format(time.RFC3339Nano),
				":lc_pk":   LifecycleGSIPK(tenantID, string(media.LifecycleFailed)),
			},
		}},
		{Put: &outboxOp},
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return err
	}
	currentAsset, getAssetErr := r.GetAsset(ctx, tenantID, mediaID, assetID)
	if getAssetErr != nil {
		return errors.Join(kv.ErrConditionFailed, getAssetErr)
	}
	currentMedia, getMediaErr := r.GetMedia(ctx, tenantID, mediaID)
	if getMediaErr != nil {
		return errors.Join(kv.ErrConditionFailed, getMediaErr)
	}
	if currentAsset.Lifecycle == media.AssetLifecycleFailed && currentMedia.Lifecycle == media.LifecycleFailed {
		return nil
	}
	return err
}

func (r *DDBRepo) replayInit(ctx context.Context, scope, inputHash string) (media.Media, media.Asset, error) {
	_, claimHash, _, metadata, err := persist.GetResultWithHashAndMetadata(ctx, r.KV, scope)
	if err != nil {
		return media.Media{}, media.Asset{}, err
	}
	if claimHash != inputHash {
		return media.Media{}, media.Asset{}, fmt.Errorf("%w: media.Init", ErrIdempotencyKeyReused)
	}
	tenantID := metadata["tenant_id"]
	mediaID := metadata["media_id"]
	assetID := metadata["asset_id"]
	if tenantID == "" || mediaID == "" || assetID == "" {
		return media.Media{}, media.Asset{}, errors.New("media.Init: claim missing tenant/media/asset metadata")
	}
	m, err := r.GetMedia(ctx, tenantID, mediaID)
	if err != nil {
		return media.Media{}, media.Asset{}, err
	}
	a, err := r.GetAsset(ctx, tenantID, mediaID, assetID)
	if err != nil {
		return media.Media{}, media.Asset{}, err
	}
	return *m, *a, nil
}
