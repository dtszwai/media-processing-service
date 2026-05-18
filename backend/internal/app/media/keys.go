package media

import (
	"fmt"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// MediaPK is the partition key for all rows under a (tenant, media).
func MediaPK(tenantID, mediaID string) string {
	return "TENANT#" + tenantID + "#MEDIA#" + mediaID
}

// MediaSK is the sort key for the parent Media row.
const MediaSK = "MEDIA"

// AssetSK returns the sort key for an Asset row.
func AssetSK(assetID string) string { return "ASSET#" + assetID }

// MetaSK returns the sort key for typed media metadata sidecars.
func MetaSK(mediaType string) string { return "META#" + strings.ToUpper(mediaType) }

// LifecycleGSIPK partitions gsi_lifecycle by tenant + lifecycle so the reaper
// can query a single lifecycle bucket without cross-tenant scatter.
func LifecycleGSIPK(tenantID string, lifecycle string) string {
	return "TENANT#" + tenantID + "#LIFECYCLE#" + lifecycle
}

// TenantMediaGSIPK partitions gsi_tenant_media by tenant so listing a tenant's
// media is a single GSI Query (no cross-tenant scatter). Co-located with the
// row writer in repo_ddb.go so the read and write sides cannot drift.
func TenantMediaGSIPK(tenantID string) string {
	return "TENANT#" + tenantID + "#MEDIA"
}

// MediaKey returns the kv.Key for the parent Media row.
func MediaKey(tenantID, mediaID string) kv.Key {
	return kv.Key{PK: MediaPK(tenantID, mediaID), SK: MediaSK}
}

// AssetKey returns the kv.Key for an Asset row.
func AssetKey(tenantID, mediaID, assetID string) kv.Key {
	return kv.Key{PK: MediaPK(tenantID, mediaID), SK: AssetSK(assetID)}
}

// RoleGSIPK partitions gsi_asset_role by (tenant, media, role) so the role
// selector ("which asset for role X under this media?") is a single Query
// rather than a ListAssets + client-side filter.
func RoleGSIPK(tenantID, mediaID string, role media.AssetRole) string {
	return "TENANT#" + tenantID + "#MEDIA#" + mediaID + "#ROLE#" + string(role)
}

// RoleGSISK encodes selector priority plus creation time. Lower priority wins:
// when two assets share a role (e.g. two PREVIEW assets staged in succession),
// the older / lower-kind-priority one sorts first and a Limit=1 Query returns
// the deterministic best-fit. The asset id is the final tiebreaker so two
// assets created in the same nanosecond still sort stably.
func RoleGSISK(kind media.AssetKind, createdAt time.Time, assetID string) string {
	return fmt.Sprintf("%03d#%s#%s", rolePriority(kind), createdAt.UTC().Format(time.RFC3339Nano), assetID)
}

// rolePriority maps an AssetKind to a 3-digit priority bucket for the role
// selector range key. Lower values are preferred when multiple assets share a
// role: an ORIGINAL beats a DERIVED of the same role so /download-url with
// AcceptFallback returns the original bytes rather than an in-progress
// derivative. GENERATED slots between ORIGINAL and DERIVED because a generated
// artifact is the canonical deliverable for its media but is not the literal
// source bytes.
func rolePriority(kind media.AssetKind) int {
	switch kind {
	case media.AssetKindOriginal:
		return 0
	case media.AssetKindGenerated:
		return 1
	case media.AssetKindDerived:
		return 3
	}
	return 999
}
