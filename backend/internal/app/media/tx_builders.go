package media

import (
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Cross-aggregate transaction builders. Other app packages (generation in
// particular) compose Media side-effects into their own TransactWrite by
// asking media to produce the ops — never by writing row shapes directly.
// This keeps media as the sole owner of its row schema while preserving the
// atomicity contract callers rely on.

// LifecycleFlipOp returns a TransactWrite-ready Update that flips a Media
// row's lifecycle. Used by generation's terminal-stage transition so the
// flip stays atomic with the job-row status change.
func LifecycleFlipOp(tenantID, mediaID string, lifecycle media.Lifecycle, now time.Time) *kv.UpdateOp {
	stamp := now.Format(time.RFC3339Nano)
	return &kv.UpdateOp{
		Key:                 MediaKey(tenantID, mediaID),
		ConditionExpression: "attribute_exists(PK)",
		UpdateExpression:    "SET lifecycle = :media_lifecycle, updated_at = :now, gsi_lifecycle_pk = :lc_pk, gsi_lifecycle_sk = :now",
		ExpressionAttributeValues: kv.Values{
			":media_lifecycle": string(lifecycle),
			":now":             stamp,
			":lc_pk":           LifecycleGSIPK(tenantID, string(lifecycle)),
		},
	}
}

// ResultArtifactRow captures the storage metadata needed to flip a
// generation-result Asset row to COMPLETE.
type ResultArtifactRow struct {
	StorageKey  string
	ContentType string
	Extension   string
	SizeBytes   int64
	SHA256Hex   string
	ETag        string
	Provenance  map[string]any
}

// CompleteResultAssetOp returns an UpdateOp that flips a generation-result
// Asset row to COMPLETE with the supplied storage metadata. The condition
// guards against a soft-deleted row racing the artifact landing.
func CompleteResultAssetOp(tenantID, mediaID, assetID string, r ResultArtifactRow, now time.Time) kv.UpdateOp {
	return kv.UpdateOp{
		Key:                 AssetKey(tenantID, mediaID, assetID),
		ConditionExpression: "attribute_exists(PK) AND lifecycle <> :deleted",
		UpdateExpression:    "SET lifecycle = :complete, storage_key = :key, content_type = :ct, extension = :ext, size_bytes = :size, sha256 = :sha, etag = :etag, provenance = :prov, updated_at = :now",
		ExpressionAttributeValues: kv.Values{
			":deleted":  string(media.AssetLifecycleDeleted),
			":complete": string(media.AssetLifecycleComplete),
			":key":      r.StorageKey,
			":ct":       r.ContentType,
			":ext":      r.Extension,
			":size":     r.SizeBytes,
			":sha":      r.SHA256Hex,
			":etag":     r.ETag,
			":prov":     r.Provenance,
			":now":      now.Format(time.RFC3339Nano),
		},
	}
}

// SubmissionPutOps returns Put ops for a (Media, Asset) pair, suitable for
// inclusion in a TransactWrite. Conditions assert neither row exists yet —
// callers use this for the first-write path during generation submit.
func SubmissionPutOps(m media.Media, a media.Asset) ([]kv.WriteOp, error) {
	mItem := newMediaRow(m)
	aItem, err := newAssetRow(a)
	if err != nil {
		return nil, err
	}
	return []kv.WriteOp{
		{Put: &kv.PutOp{Item: mItem, ConditionExpression: "attribute_not_exists(PK)"}},
		{Put: &kv.PutOp{Item: aItem, ConditionExpression: "attribute_not_exists(SK)"}},
	}, nil
}
