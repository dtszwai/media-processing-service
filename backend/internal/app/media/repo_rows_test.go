package media

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

type expectedMediaRow struct {
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

type expectedAssetRow struct {
	PK             string               `dynamodbav:"PK"`
	SK             string               `dynamodbav:"SK"`
	ItemType       string               `dynamodbav:"item_type"`
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
	DesiredSpec    *expectedDesiredSpec `dynamodbav:"desired_spec,omitempty"`
	Provenance     *expectedProvenance  `dynamodbav:"provenance,omitempty"`
	Attempts       uint32               `dynamodbav:"attempts,omitempty"`
	ErrorCode      string               `dynamodbav:"error_code,omitempty"`
	ErrorMessage   string               `dynamodbav:"error_message,omitempty"`
	CreatedAt      time.Time            `dynamodbav:"created_at"`
	UpdatedAt      time.Time            `dynamodbav:"updated_at"`
}

type expectedImageMetadataRow struct {
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

type expectedDesiredSpec struct {
	OutputFormat string   `dynamodbav:"output_format,omitempty"`
	Width        uint32   `dynamodbav:"width,omitempty"`
	Height       uint32   `dynamodbav:"height,omitempty"`
	Tags         []string `dynamodbav:"tags,omitempty"`
}

type expectedProvenance struct {
	Provider    string `dynamodbav:"provider,omitempty"`
	Model       string `dynamodbav:"model,omitempty"`
	AIGenerated bool   `dynamodbav:"ai_generated,omitempty"`
	Disclosure  string `dynamodbav:"disclosure,omitempty"`
	Watermark   string `dynamodbav:"watermark,omitempty"`
	Safety      string `dynamodbav:"safety,omitempty"`
}

func TestMediaRowPreservesDynamoDBShape(t *testing.T) {
	created := time.Date(2026, 5, 17, 10, 11, 12, 13, time.UTC)
	updated := created.Add(time.Minute)
	deleted := created.Add(2 * time.Minute)
	expires := created.Add(24 * time.Hour)
	m := media.Media{
		ID:              "med_1",
		TenantID:        "tenant_1",
		OwnerUserID:     "user_1",
		Visibility:      media.VisibilityOwnerPrivate,
		Origin:          media.OriginUpload,
		Type:            media.TypeImage,
		Lifecycle:       media.LifecycleRunning,
		OriginalAssetID: "ast_1",
		WebhookURL:      "https://example.com/hook",
		CreatedAt:       created,
		UpdatedAt:       updated,
		DeletedAt:       &deleted,
		ExpiresAt:       &expires,
	}

	want := expectedMediaRow{
		PK:               MediaPK(m.TenantID, m.ID),
		SK:               MediaSK,
		ItemType:         "MEDIA",
		GSITenantMediaPK: TenantMediaGSIPK(m.TenantID),
		GSITenantMediaSK: m.CreatedAt.UTC().Format(time.RFC3339Nano) + "#" + m.ID,
		GSILifecyclePK:   LifecycleGSIPK(m.TenantID, string(m.Lifecycle)),
		GSILifecycleSK:   m.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ID:               m.ID,
		TenantID:         m.TenantID,
		OwnerUserID:      m.OwnerUserID,
		Visibility:       m.Visibility,
		Origin:           m.Origin,
		Type:             m.Type,
		Lifecycle:        m.Lifecycle,
		OriginalAssetID:  m.OriginalAssetID,
		WebhookURL:       m.WebhookURL,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		DeletedAt:        m.DeletedAt,
		ExpiresAt:        m.ExpiresAt,
	}
	assertSameDynamoDBShape(t, newMediaRow(m), want)

	var decoded mediaRow
	roundTripRow(t, newMediaRow(m), &decoded)
	if got := decoded.toDomain(); !reflect.DeepEqual(got, m) {
		t.Fatalf("round trip media = %+v, want %+v", got, m)
	}
}

func TestAssetRowPreservesDynamoDBShape(t *testing.T) {
	created := time.Date(2026, 5, 17, 10, 11, 12, 13, time.UTC)
	updated := created.Add(time.Minute)
	a := media.Asset{
		ID:            "ast_1",
		MediaID:       "med_1",
		TenantID:      "tenant_1",
		Kind:          media.AssetKindDerived,
		Role:          media.AssetRolePreview,
		Operation:     media.AssetOperationImageResize,
		Lifecycle:     media.AssetLifecycleFailed,
		SourceAssetID: "ast_original",
		StorageKey:    "tenant_1/med_1/assets/ast_1.webp",
		ContentType:   "image/webp",
		Extension:     "webp",
		SizeBytes:     1234,
		SHA256:        "abc123",
		ETag:          "etag-1",
		DesiredSpec:   &media.DesiredSpec{OutputFormat: "webp", Width: 1024, Height: 768, Tags: []string{"preview", "public"}},
		Provenance:    &media.Provenance{Provider: "codex", Model: "image-1", AIGenerated: true, Disclosure: "ai", Watermark: "wm", Safety: "passed"},
		Attempts:      2,
		ErrorCode:     "DERIVE_FAILED",
		ErrorMessage:  "resize failed",
		CreatedAt:     created,
		UpdatedAt:     updated,
	}

	want := expectedAssetRow{
		PK:             MediaPK(a.TenantID, a.MediaID),
		SK:             AssetSK(a.ID),
		ItemType:       "ASSET",
		GSIAssetRolePK: RoleGSIPK(a.TenantID, a.MediaID, a.Role),
		GSIAssetRoleSK: RoleGSISK(a.Kind, a.CreatedAt, a.ID),
		ID:             a.ID,
		MediaID:        a.MediaID,
		TenantID:       a.TenantID,
		Kind:           a.Kind,
		Role:           a.Role,
		Operation:      a.Operation,
		Lifecycle:      a.Lifecycle,
		SourceAssetID:  a.SourceAssetID,
		StorageKey:     a.StorageKey,
		ContentType:    a.ContentType,
		Extension:      a.Extension,
		SizeBytes:      a.SizeBytes,
		SHA256:         a.SHA256,
		ETag:           a.ETag,
		DesiredSpec:    &expectedDesiredSpec{OutputFormat: "webp", Width: 1024, Height: 768, Tags: []string{"preview", "public"}},
		Provenance:     &expectedProvenance{Provider: "codex", Model: "image-1", AIGenerated: true, Disclosure: "ai", Watermark: "wm", Safety: "passed"},
		Attempts:       a.Attempts,
		ErrorCode:      a.ErrorCode,
		ErrorMessage:   a.ErrorMessage,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
	assertSameDynamoDBShape(t, assetRowFromDomain(a), want)

	var decoded assetRow
	roundTripRow(t, assetRowFromDomain(a), &decoded)
	if got := decoded.toDomain(); !reflect.DeepEqual(got, a) {
		t.Fatalf("round trip asset = %+v, want %+v", got, a)
	}
}

func TestImageMetadataRowPreservesDynamoDBShape(t *testing.T) {
	created := time.Date(2026, 5, 17, 10, 11, 12, 13, time.UTC)
	meta := media.ImageMetadata{
		TenantID:    "tenant_1",
		MediaID:     "med_1",
		AssetID:     "ast_1",
		Width:       640,
		Height:      480,
		Format:      "png",
		ContentType: "image/png",
		CreatedAt:   created,
		UpdatedAt:   created.Add(time.Minute),
	}

	want := expectedImageMetadataRow{
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
	assertSameDynamoDBShape(t, newImageMetadataRow(meta), want)

	var decoded imageMetadataRow
	roundTripRow(t, newImageMetadataRow(meta), &decoded)
	if got := decoded.toDomain(); !reflect.DeepEqual(got, meta) {
		t.Fatalf("round trip metadata = %+v, want %+v", got, meta)
	}
}

func TestCompleteAssetUpdateStoresSizeBytesAsNumber(t *testing.T) {
	repo := &DDBRepo{}
	op := repo.completeAssetUpdate(media.Asset{
		ID:          "ast_1",
		MediaID:     "med_1",
		TenantID:    "tenant_1",
		SizeBytes:   1234,
		ContentType: "image/png",
	}, "med_1", "tenant_1", time.Now().UTC())

	if _, ok := op.ExpressionAttributeValues[":sz"].(uint64); !ok {
		t.Fatalf("size_bytes expression value type = %T, want uint64", op.ExpressionAttributeValues[":sz"])
	}
	av, err := attributevalue.Marshal(op.ExpressionAttributeValues[":sz"])
	if err != nil {
		t.Fatalf("marshal size_bytes: %v", err)
	}
	if _, ok := av.(*types.AttributeValueMemberN); !ok {
		t.Fatalf("size_bytes DynamoDB attribute type = %T, want number", av)
	}
}

func assertSameDynamoDBShape(t *testing.T, got, want any) {
	t.Helper()
	gotAV := mustMarshalRow(t, got)
	wantAV := mustMarshalRow(t, want)
	if !reflect.DeepEqual(gotAV, wantAV) {
		t.Fatalf("DynamoDB shape mismatch\ngot keys:  %v\nwant keys: %v\ngot:  %#v\nwant: %#v",
			avKeys(gotAV), avKeys(wantAV), gotAV, wantAV)
	}
}

func roundTripRow(t *testing.T, row, out any) {
	t.Helper()
	if err := attributevalue.UnmarshalMap(mustMarshalRow(t, row), out); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
}

func mustMarshalRow(t *testing.T, row any) map[string]types.AttributeValue {
	t.Helper()
	av, err := attributevalue.MarshalMap(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	return av
}

func avKeys(av map[string]types.AttributeValue) []string {
	keys := make([]string, 0, len(av))
	for k := range av {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
