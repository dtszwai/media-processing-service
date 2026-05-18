package media

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	domainmedia "github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	commonpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/common/v1"
	mediapb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/media/v1"
)

func initUploadToProto(out *mediaapp.InitOutput) *mediapb.InitPresignedUploadResponse {
	if out == nil {
		return &mediapb.InitPresignedUploadResponse{}
	}
	return &mediapb.InitPresignedUploadResponse{
		MediaId:    out.MediaID,
		AssetId:    out.AssetID,
		StorageKey: out.StorageKey,
		UploadUrl:  out.UploadURL,
		Method:     out.Method,
		Headers:    out.Headers,
		ExpiresIn:  uint32(out.ExpiresIn),
		ExpiresAt:  timestamppb.New(out.ExpiresAt),
	}
}

func refreshUploadToProto(out *mediaapp.InitOutput) *mediapb.RefreshPresignedUploadResponse {
	if out == nil {
		return &mediapb.RefreshPresignedUploadResponse{}
	}
	return &mediapb.RefreshPresignedUploadResponse{
		MediaId:    out.MediaID,
		AssetId:    out.AssetID,
		StorageKey: out.StorageKey,
		UploadUrl:  out.UploadURL,
		Method:     out.Method,
		Headers:    out.Headers,
		ExpiresIn:  uint32(out.ExpiresIn),
		ExpiresAt:  timestamppb.New(out.ExpiresAt),
	}
}

func mediaToProto(m domainmedia.Media) *mediapb.Media {
	return &mediapb.Media{
		MediaId:         m.ID,
		TenantId:        m.TenantID,
		OwnerUserId:     m.OwnerUserID,
		Origin:          originToProto(m.Origin),
		MediaType:       mediaTypeToProto(m.Type),
		Lifecycle:       mediaLifecycleToProto(m.Lifecycle),
		OriginalAssetId: m.OriginalAssetID,
		WebhookUrl:      m.WebhookURL,
		CreatedAt:       protoutil.OptTimestamp(m.CreatedAt),
		UpdatedAt:       protoutil.OptTimestamp(m.UpdatedAt),
		DeletedAt:       protoutil.OptTimestampPtr(m.DeletedAt),
		ExpiresAt:       protoutil.OptTimestampPtr(m.ExpiresAt),
		Visibility:      visibilityToProto(m.Visibility),
	}
}

func assetToProto(a domainmedia.Asset) *mediapb.Asset {
	out := &mediapb.Asset{
		AssetId:       a.ID,
		MediaId:       a.MediaID,
		TenantId:      a.TenantID,
		Kind:          assetKindToProto(a.Kind),
		Role:          assetRoleToProto(a.Role),
		Operation:     assetOperationToProto(a.Operation),
		Lifecycle:     assetLifecycleToProto(a.Lifecycle),
		SourceAssetId: a.SourceAssetID,
		StorageKey:    a.StorageKey,
		ContentType:   a.ContentType,
		Extension:     a.Extension,
		SizeBytes:     a.SizeBytes,
		Sha256:        a.SHA256,
		Attempts:      a.Attempts,
		ErrorCode:     a.ErrorCode,
		ErrorMessage:  a.ErrorMessage,
		CreatedAt:     protoutil.OptTimestamp(a.CreatedAt),
		UpdatedAt:     protoutil.OptTimestamp(a.UpdatedAt),
	}
	if a.DesiredSpec != nil {
		out.DesiredSpec = &mediapb.DesiredSpec{
			OutputFormat: a.DesiredSpec.OutputFormat,
			Width:        a.DesiredSpec.Width,
			Height:       a.DesiredSpec.Height,
			Tags:         append([]string(nil), a.DesiredSpec.Tags...),
		}
	}
	if a.Provenance != nil {
		out.Provenance = &mediapb.Provenance{
			Provider:    a.Provenance.Provider,
			Model:       a.Provenance.Model,
			AiGenerated: a.Provenance.AIGenerated,
			Disclosure:  a.Provenance.Disclosure,
			Watermark:   a.Provenance.Watermark,
			Safety:      a.Provenance.Safety,
		}
	}
	return out
}

func mediaTypeFromProto(t commonpb.MediaType) string {
	switch t {
	case commonpb.MediaType_MEDIA_TYPE_IMAGE:
		return string(domainmedia.TypeImage)
	case commonpb.MediaType_MEDIA_TYPE_AUDIO:
		return string(domainmedia.TypeAudio)
	default:
		return ""
	}
}

func mediaTypeToProto(t domainmedia.Type) commonpb.MediaType {
	switch t {
	case domainmedia.TypeImage:
		return commonpb.MediaType_MEDIA_TYPE_IMAGE
	case domainmedia.TypeAudio:
		return commonpb.MediaType_MEDIA_TYPE_AUDIO
	default:
		return commonpb.MediaType_MEDIA_TYPE_UNSPECIFIED
	}
}

func originFromProto(origin commonpb.Origin) string {
	switch origin {
	case commonpb.Origin_ORIGIN_UPLOAD:
		return string(domainmedia.OriginUpload)
	case commonpb.Origin_ORIGIN_GENERATED:
		return string(domainmedia.OriginGenerated)
	default:
		return ""
	}
}

func originToProto(origin domainmedia.Origin) commonpb.Origin {
	switch origin {
	case domainmedia.OriginUpload:
		return commonpb.Origin_ORIGIN_UPLOAD
	case domainmedia.OriginGenerated:
		return commonpb.Origin_ORIGIN_GENERATED
	default:
		return commonpb.Origin_ORIGIN_UNSPECIFIED
	}
}

func visibilityToProto(v domainmedia.Visibility) mediapb.Visibility {
	switch v {
	case domainmedia.VisibilityOwnerPrivate:
		return mediapb.Visibility_VISIBILITY_OWNER_PRIVATE
	case domainmedia.VisibilityTenantShared:
		return mediapb.Visibility_VISIBILITY_TENANT_SHARED
	default:
		return mediapb.Visibility_VISIBILITY_UNSPECIFIED
	}
}

func mediaLifecycleToProto(l domainmedia.Lifecycle) mediapb.MediaLifecycle {
	switch l {
	case domainmedia.LifecyclePending:
		return mediapb.MediaLifecycle_MEDIA_LIFECYCLE_PENDING
	case domainmedia.LifecycleRunning:
		return mediapb.MediaLifecycle_MEDIA_LIFECYCLE_RUNNING
	case domainmedia.LifecycleComplete:
		return mediapb.MediaLifecycle_MEDIA_LIFECYCLE_COMPLETE
	case domainmedia.LifecycleFailed:
		return mediapb.MediaLifecycle_MEDIA_LIFECYCLE_FAILED
	case domainmedia.LifecycleDeleted:
		return mediapb.MediaLifecycle_MEDIA_LIFECYCLE_DELETED
	default:
		return mediapb.MediaLifecycle_MEDIA_LIFECYCLE_UNSPECIFIED
	}
}

func assetKindToProto(kind domainmedia.AssetKind) mediapb.AssetKind {
	switch kind {
	case domainmedia.AssetKindOriginal:
		return mediapb.AssetKind_ASSET_KIND_ORIGINAL
	case domainmedia.AssetKindDerived:
		return mediapb.AssetKind_ASSET_KIND_DERIVED
	case domainmedia.AssetKindGenerated:
		return mediapb.AssetKind_ASSET_KIND_GENERATED
	default:
		return mediapb.AssetKind_ASSET_KIND_UNSPECIFIED
	}
}

func assetRoleToDomain(role mediapb.AssetRole) domainmedia.AssetRole {
	switch role {
	case mediapb.AssetRole_ASSET_ROLE_ORIGINAL:
		return domainmedia.AssetRoleOriginal
	case mediapb.AssetRole_ASSET_ROLE_THUMBNAIL:
		return domainmedia.AssetRoleThumbnail
	case mediapb.AssetRole_ASSET_ROLE_PREVIEW:
		return domainmedia.AssetRolePreview
	case mediapb.AssetRole_ASSET_ROLE_DOWNLOAD:
		return domainmedia.AssetRoleDownload
	case mediapb.AssetRole_ASSET_ROLE_FINAL:
		return domainmedia.AssetRoleFinal
	default:
		return ""
	}
}

func assetRoleToProto(role domainmedia.AssetRole) mediapb.AssetRole {
	switch role {
	case domainmedia.AssetRoleOriginal:
		return mediapb.AssetRole_ASSET_ROLE_ORIGINAL
	case domainmedia.AssetRoleThumbnail:
		return mediapb.AssetRole_ASSET_ROLE_THUMBNAIL
	case domainmedia.AssetRolePreview:
		return mediapb.AssetRole_ASSET_ROLE_PREVIEW
	case domainmedia.AssetRoleDownload:
		return mediapb.AssetRole_ASSET_ROLE_DOWNLOAD
	case domainmedia.AssetRoleFinal:
		return mediapb.AssetRole_ASSET_ROLE_FINAL
	default:
		return mediapb.AssetRole_ASSET_ROLE_UNSPECIFIED
	}
}

func assetOperationToDomain(op mediapb.AssetOperation) string {
	switch op {
	case mediapb.AssetOperation_ASSET_OPERATION_IMAGE_THUMBNAIL:
		return "thumbnail"
	default:
		return ""
	}
}

func assetOperationFromName(op string) mediapb.AssetOperation {
	switch op {
	case "thumbnail":
		return mediapb.AssetOperation_ASSET_OPERATION_IMAGE_THUMBNAIL
	default:
		return mediapb.AssetOperation_ASSET_OPERATION_UNSPECIFIED
	}
}

func assetOperationToProto(op domainmedia.AssetOperation) mediapb.AssetOperation {
	switch op {
	case domainmedia.AssetOperationImageResize:
		return mediapb.AssetOperation_ASSET_OPERATION_IMAGE_RESIZE
	case domainmedia.AssetOperationImageThumbnail:
		return mediapb.AssetOperation_ASSET_OPERATION_IMAGE_THUMBNAIL
	case domainmedia.AssetOperationGenerationFinal:
		return mediapb.AssetOperation_ASSET_OPERATION_GENERATION_FINAL
	default:
		return mediapb.AssetOperation_ASSET_OPERATION_UNSPECIFIED
	}
}

func assetLifecycleFromName(lifecycle string) mediapb.AssetLifecycle {
	return assetLifecycleToProto(domainmedia.AssetLifecycle(lifecycle))
}

func assetLifecycleToProto(lifecycle domainmedia.AssetLifecycle) mediapb.AssetLifecycle {
	switch lifecycle {
	case domainmedia.AssetLifecyclePendingUpload:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_PENDING_UPLOAD
	case domainmedia.AssetLifecyclePending:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_PENDING
	case domainmedia.AssetLifecycleProcessing:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_PROCESSING
	case domainmedia.AssetLifecycleComplete:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_COMPLETE
	case domainmedia.AssetLifecycleFailed:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_FAILED
	case domainmedia.AssetLifecycleDeleted:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_DELETED
	default:
		return mediapb.AssetLifecycle_ASSET_LIFECYCLE_UNSPECIFIED
	}
}
