package media

import (
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

type mediaRow struct {
	PK               string           `dynamodbav:"PK"`
	SK               string           `dynamodbav:"SK"`
	ItemType         string           `dynamodbav:"item_type"`
	GSITenantMediaPK string           `dynamodbav:"gsi_tenant_media_pk"`
	GSITenantMediaSK string           `dynamodbav:"gsi_tenant_media_sk"`
	GSILifecyclePK   string           `dynamodbav:"gsi_lifecycle_pk"`
	GSILifecycleSK   string           `dynamodbav:"gsi_lifecycle_sk"`
	ID               string           `dynamodbav:"id"`
	TenantID         string           `dynamodbav:"tenant_id"`
	OwnerUserID      string           `dynamodbav:"owner_user_id,omitempty"`
	Visibility       media.Visibility `dynamodbav:"visibility,omitempty"`
	Origin           media.Origin     `dynamodbav:"origin"`
	Type             media.Type       `dynamodbav:"media_type"`
	Lifecycle        media.Lifecycle  `dynamodbav:"lifecycle"`
	OriginalAssetID  string           `dynamodbav:"original_asset_id,omitempty"`
	WebhookURL       string           `dynamodbav:"webhook_url,omitempty"`
	CreatedAt        time.Time        `dynamodbav:"created_at"`
	UpdatedAt        time.Time        `dynamodbav:"updated_at"`
	DeletedAt        *time.Time       `dynamodbav:"deleted_at,omitempty"`
	ExpiresAt        *time.Time       `dynamodbav:"expires_at,omitempty"`
}

type assetRow struct {
	PK       string `dynamodbav:"PK"`
	SK       string `dynamodbav:"SK"`
	ItemType string `dynamodbav:"item_type"`
	// GSIAssetRolePK / GSIAssetRoleSK route role selectors via gsi_asset_role
	// instead of presigning an arbitrary asset id. Empty when Role is unset so
	// rows without a meaningful role (e.g. staging refs) stay off the index.
	GSIAssetRolePK string               `dynamodbav:"gsi_asset_role_pk,omitempty"`
	GSIAssetRoleSK string               `dynamodbav:"gsi_asset_role_sk,omitempty"`
	ID             string               `dynamodbav:"id"`
	MediaID        string               `dynamodbav:"media_id"`
	TenantID       string               `dynamodbav:"tenant_id"`
	Kind           media.AssetKind      `dynamodbav:"kind"`
	Role           media.AssetRole      `dynamodbav:"role"`
	Operation      media.AssetOperation `dynamodbav:"operation,omitempty"`
	Lifecycle      media.AssetLifecycle `dynamodbav:"lifecycle"`
	SourceAssetID  string               `dynamodbav:"source_asset_id,omitempty"`
	StorageKey     string               `dynamodbav:"storage_key,omitempty"`
	ContentType    string               `dynamodbav:"content_type,omitempty"`
	Extension      string               `dynamodbav:"extension,omitempty"`
	SizeBytes      uint64               `dynamodbav:"size_bytes,omitempty"`
	SHA256         string               `dynamodbav:"sha256,omitempty"`
	ETag           string               `dynamodbav:"etag,omitempty"`
	DesiredSpec    *desiredSpecRow      `dynamodbav:"desired_spec,omitempty"`
	Provenance     *provenanceRow       `dynamodbav:"provenance,omitempty"`
	Attempts       uint32               `dynamodbav:"attempts,omitempty"`
	ErrorCode      string               `dynamodbav:"error_code,omitempty"`
	ErrorMessage   string               `dynamodbav:"error_message,omitempty"`
	CreatedAt      time.Time            `dynamodbav:"created_at"`
	UpdatedAt      time.Time            `dynamodbav:"updated_at"`
}

type imageMetadataRow struct {
	PK          string    `dynamodbav:"PK"`
	SK          string    `dynamodbav:"SK"`
	ItemType    string    `dynamodbav:"item_type"`
	TenantID    string    `dynamodbav:"tenant_id"`
	MediaID     string    `dynamodbav:"media_id"`
	AssetID     string    `dynamodbav:"asset_id"`
	Width       uint32    `dynamodbav:"width"`
	Height      uint32    `dynamodbav:"height"`
	Format      string    `dynamodbav:"format"`
	ContentType string    `dynamodbav:"content_type,omitempty"`
	CreatedAt   time.Time `dynamodbav:"created_at"`
	UpdatedAt   time.Time `dynamodbav:"updated_at"`
}

type desiredSpecRow struct {
	OutputFormat string   `dynamodbav:"output_format,omitempty"`
	Width        uint32   `dynamodbav:"width,omitempty"`
	Height       uint32   `dynamodbav:"height,omitempty"`
	Tags         []string `dynamodbav:"tags,omitempty"`
}

type provenanceRow struct {
	Provider    string `dynamodbav:"provider,omitempty"`
	Model       string `dynamodbav:"model,omitempty"`
	AIGenerated bool   `dynamodbav:"ai_generated,omitempty"`
	Disclosure  string `dynamodbav:"disclosure,omitempty"`
	Watermark   string `dynamodbav:"watermark,omitempty"`
	Safety      string `dynamodbav:"safety,omitempty"`
}

func newMediaRow(m media.Media) mediaRow {
	row := mediaRowFromDomain(m)
	row.PK = MediaPK(m.TenantID, m.ID)
	row.SK = MediaSK
	row.ItemType = "MEDIA"
	row.GSITenantMediaPK = TenantMediaGSIPK(m.TenantID)
	row.GSITenantMediaSK = m.CreatedAt.UTC().Format(time.RFC3339Nano) + "#" + m.ID
	row.GSILifecyclePK = LifecycleGSIPK(m.TenantID, string(m.Lifecycle))
	row.GSILifecycleSK = m.UpdatedAt.UTC().Format(time.RFC3339Nano)
	return row
}

func mediaRowFromDomain(m media.Media) mediaRow {
	return mediaRow{
		ID:              m.ID,
		TenantID:        m.TenantID,
		OwnerUserID:     m.OwnerUserID,
		Visibility:      m.Visibility,
		Origin:          m.Origin,
		Type:            m.Type,
		Lifecycle:       m.Lifecycle,
		OriginalAssetID: m.OriginalAssetID,
		WebhookURL:      m.WebhookURL,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
		DeletedAt:       m.DeletedAt,
		ExpiresAt:       m.ExpiresAt,
	}
}

func (r mediaRow) toDomain() media.Media {
	return media.Media{
		ID:              r.ID,
		TenantID:        r.TenantID,
		OwnerUserID:     r.OwnerUserID,
		Visibility:      r.Visibility,
		Origin:          r.Origin,
		Type:            r.Type,
		Lifecycle:       r.Lifecycle,
		OriginalAssetID: r.OriginalAssetID,
		WebhookURL:      r.WebhookURL,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		DeletedAt:       r.DeletedAt,
		ExpiresAt:       r.ExpiresAt,
	}
}

func assetRowFromDomain(a media.Asset) assetRow {
	row := assetRow{
		PK:            MediaPK(a.TenantID, a.MediaID),
		SK:            AssetSK(a.ID),
		ItemType:      "ASSET",
		ID:            a.ID,
		MediaID:       a.MediaID,
		TenantID:      a.TenantID,
		Kind:          a.Kind,
		Role:          a.Role,
		Operation:     a.Operation,
		Lifecycle:     a.Lifecycle,
		SourceAssetID: a.SourceAssetID,
		StorageKey:    a.StorageKey,
		ContentType:   a.ContentType,
		Extension:     a.Extension,
		SizeBytes:     a.SizeBytes,
		SHA256:        a.SHA256,
		ETag:          a.ETag,
		DesiredSpec:   desiredSpecRowFromDomain(a.DesiredSpec),
		Provenance:    provenanceRowFromDomain(a.Provenance),
		Attempts:      a.Attempts,
		ErrorCode:     a.ErrorCode,
		ErrorMessage:  a.ErrorMessage,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
	// Rows without a role aren't selector targets, so they stay off the index.
	// Skipping them keeps the GSI shape honest — every (tenant, media, role)
	// partition reflects a real role choice rather than a sentinel value.
	if a.Role != "" {
		row.GSIAssetRolePK = RoleGSIPK(a.TenantID, a.MediaID, a.Role)
		row.GSIAssetRoleSK = RoleGSISK(a.Kind, a.CreatedAt, a.ID)
	}
	return row
}

func (r assetRow) toDomain() media.Asset {
	return media.Asset{
		ID:            r.ID,
		MediaID:       r.MediaID,
		TenantID:      r.TenantID,
		Kind:          r.Kind,
		Role:          r.Role,
		Operation:     r.Operation,
		Lifecycle:     r.Lifecycle,
		SourceAssetID: r.SourceAssetID,
		StorageKey:    r.StorageKey,
		ContentType:   r.ContentType,
		Extension:     r.Extension,
		SizeBytes:     r.SizeBytes,
		SHA256:        r.SHA256,
		ETag:          r.ETag,
		DesiredSpec:   r.DesiredSpec.toDomain(),
		Provenance:    r.Provenance.toDomain(),
		Attempts:      r.Attempts,
		ErrorCode:     r.ErrorCode,
		ErrorMessage:  r.ErrorMessage,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func newImageMetadataRow(meta media.ImageMetadata) imageMetadataRow {
	return imageMetadataRow{
		PK:          MediaPK(meta.TenantID, meta.MediaID),
		SK:          MetaSK(string(media.TypeImage)),
		ItemType:    "IMAGE_METADATA",
		TenantID:    meta.TenantID,
		MediaID:     meta.MediaID,
		AssetID:     meta.AssetID,
		Width:       meta.Width,
		Height:      meta.Height,
		Format:      meta.Format,
		ContentType: meta.ContentType,
		CreatedAt:   meta.CreatedAt,
		UpdatedAt:   meta.UpdatedAt,
	}
}

func (r imageMetadataRow) toDomain() media.ImageMetadata {
	return media.ImageMetadata{
		TenantID:    r.TenantID,
		MediaID:     r.MediaID,
		AssetID:     r.AssetID,
		Width:       r.Width,
		Height:      r.Height,
		Format:      r.Format,
		ContentType: r.ContentType,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func desiredSpecRowFromDomain(spec *media.DesiredSpec) *desiredSpecRow {
	if spec == nil {
		return nil
	}
	return &desiredSpecRow{
		OutputFormat: spec.OutputFormat,
		Width:        spec.Width,
		Height:       spec.Height,
		Tags:         spec.Tags,
	}
}

func (r *desiredSpecRow) toDomain() *media.DesiredSpec {
	if r == nil {
		return nil
	}
	return &media.DesiredSpec{
		OutputFormat: r.OutputFormat,
		Width:        r.Width,
		Height:       r.Height,
		Tags:         r.Tags,
	}
}

func provenanceRowFromDomain(prov *media.Provenance) *provenanceRow {
	if prov == nil {
		return nil
	}
	return &provenanceRow{
		Provider:    prov.Provider,
		Model:       prov.Model,
		AIGenerated: prov.AIGenerated,
		Disclosure:  prov.Disclosure,
		Watermark:   prov.Watermark,
		Safety:      prov.Safety,
	}
}

func (r *provenanceRow) toDomain() *media.Provenance {
	if r == nil {
		return nil
	}
	return &media.Provenance{
		Provider:    r.Provider,
		Model:       r.Model,
		AIGenerated: r.AIGenerated,
		Disclosure:  r.Disclosure,
		Watermark:   r.Watermark,
		Safety:      r.Safety,
	}
}

func newAssetRow(a media.Asset) (assetRow, error) {
	if a.ID == "" || a.MediaID == "" || a.TenantID == "" {
		return assetRow{}, fmt.Errorf("%w: asset id, media_id, tenant_id required", ErrInvalidInput)
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	a.UpdatedAt = time.Now().UTC()
	if a.StorageKey == "" {
		a.StorageKey = media.StorageKey(a.TenantID, a.MediaID, a.ID, a.Extension)
	}
	return assetRowFromDomain(a), nil
}
