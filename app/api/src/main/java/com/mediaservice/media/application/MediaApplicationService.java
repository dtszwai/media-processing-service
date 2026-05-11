package com.mediaservice.media.application;

import com.mediaservice.analytics.application.AnalyticsService;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaSource;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.common.model.ProcessingJob;
import com.mediaservice.common.model.ProcessingJobStatus;
import com.mediaservice.media.api.dto.CreateAssetOutput;
import com.mediaservice.media.api.dto.CreateAssetRequest;
import com.mediaservice.media.api.dto.InitUploadRequest;
import com.mediaservice.media.api.dto.InitUploadResponse;
import com.mediaservice.media.domain.service.DocumentValidationService;
import com.mediaservice.media.domain.service.ImageValidationService;
import com.mediaservice.media.domain.service.MediaTypeResolver;
import com.mediaservice.media.domain.service.ThumbnailService;
import com.mediaservice.media.infrastructure.messaging.MediaEventPublisher;
import com.mediaservice.media.infrastructure.persistence.MediaAssetDynamoDbRepository;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import com.mediaservice.media.infrastructure.persistence.ProcessingJobDynamoDbRepository;
import com.mediaservice.media.infrastructure.storage.S3StorageService;
import com.mediaservice.shared.auth.AuthorizationService;
import com.mediaservice.shared.auth.TenantContext;
import com.mediaservice.shared.cache.CacheInvalidationService;
import com.mediaservice.shared.cache.MultiLevelCacheOrchestrator;
import com.mediaservice.shared.config.properties.MediaProperties;
import com.mediaservice.shared.http.error.MediaGoneException;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import java.io.IOException;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

@Slf4j
@Service
public class MediaApplicationService {
  private static final String INVALID_FILE_TYPE_MESSAGE = "Invalid file type. Only images and PDFs are supported.";
  private static final String PRESIGNED_UPLOAD_METHOD = "PUT";
  private static final List<String> ORIGINAL_ASSET_TAGS = List.of("original");

  private final MediaDynamoDbRepository mediaRepository;
  private final MediaAssetDynamoDbRepository assetRepository;
  private final ProcessingJobDynamoDbRepository jobRepository;
  private final S3StorageService s3Service;
  private final MediaEventPublisher eventPublisher;
  private final MediaProperties mediaProperties;
  private final ImageValidationService imageValidationService;
  private final DocumentValidationService documentValidationService;
  private final MediaTypeResolver mediaTypeResolver;
  private final ThumbnailService thumbnailService;
  private final CacheInvalidationService cacheInvalidationService;
  private final MultiLevelCacheOrchestrator cacheOrchestrator;
  private final AnalyticsService analyticsService;
  private final AuthorizationService authorizationService;
  private final Tracer tracer;
  private final LongCounter uploadSuccessCounter;
  private final LongCounter uploadFailureCounter;

  public MediaApplicationService(MediaDynamoDbRepository mediaRepository,
      MediaAssetDynamoDbRepository assetRepository,
      ProcessingJobDynamoDbRepository jobRepository,
      S3StorageService s3Service,
      MediaEventPublisher eventPublisher,
      MediaProperties mediaProperties,
      ImageValidationService imageValidationService,
      DocumentValidationService documentValidationService,
      MediaTypeResolver mediaTypeResolver,
      ThumbnailService thumbnailService,
      CacheInvalidationService cacheInvalidationService,
      MultiLevelCacheOrchestrator cacheOrchestrator,
      AnalyticsService analyticsService,
      AuthorizationService authorizationService,
      Tracer tracer,
      Meter meter) {
    this.mediaRepository = mediaRepository;
    this.assetRepository = assetRepository;
    this.jobRepository = jobRepository;
    this.s3Service = s3Service;
    this.eventPublisher = eventPublisher;
    this.mediaProperties = mediaProperties;
    this.imageValidationService = imageValidationService;
    this.documentValidationService = documentValidationService;
    this.mediaTypeResolver = mediaTypeResolver;
    this.thumbnailService = thumbnailService;
    this.cacheInvalidationService = cacheInvalidationService;
    this.cacheOrchestrator = cacheOrchestrator;
    this.analyticsService = analyticsService;
    this.authorizationService = authorizationService;
    this.tracer = tracer;
    this.uploadSuccessCounter = meter.counterBuilder("media.upload.success")
        .setDescription("Count of successful media uploads")
        .build();
    this.uploadFailureCounter = meter.counterBuilder("media.upload.failure")
        .setDescription("Count of failed media uploads")
        .build();
  }

  public Media uploadMedia(MultipartFile file, String mediaType) throws IOException {
    String contentType = file.getContentType();
    MediaType resolvedType = mediaTypeResolver.resolve(MediaType.fromString(mediaType), contentType,
        file.getOriginalFilename());
    if (resolvedType == null) {
      throw new IllegalArgumentException(INVALID_FILE_TYPE_MESSAGE);
    }

    if (resolvedType == MediaType.IMAGE) {
      imageValidationService.validateImage(file);
    } else if (resolvedType == MediaType.DOCUMENT) {
      documentValidationService.validatePdf(file);
    }

    Span span = tracer.spanBuilder("upload-media-file")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      String assetId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      String originalName = file.getOriginalFilename();
      String fileName = (originalName == null || originalName.isEmpty()) ? "upload" : originalName;
      span.setAttribute("file.name", fileName);
      span.setAttribute("media.type", resolvedType.getValue());
      String tenantId = TenantContext.getTenantId();
      String userId = TenantContext.getUserId();

      s3Service.uploadAsset(tenantId, mediaId, assetId, fileName, file);

      Media media = Media.builder()
          .mediaId(mediaId)
          .tenantId(tenantId)
          .userId(userId)
          .size(file.getSize())
          .name(fileName)
          .mimetype(contentType)
          .mediaType(resolvedType)
          .source(MediaSource.UPLOAD)
          .status(MediaStatus.COMPLETE)
          .originalAssetId(assetId)
          .createdAt(Instant.now())
          .build();

      try {
        mediaRepository.createMedia(media);
        assetRepository.createAsset(buildOriginalAsset(
            assetId,
            mediaId,
            tenantId,
            fileName,
            contentType,
            file.getSize(),
            AssetStatus.COMPLETE));
      } catch (Exception e) {
        try {
          s3Service.deleteAsset(tenantId, mediaId, assetId, fileName);
        } catch (Exception cleanupErr) {
          log.warn("Failed to clean up S3 asset after DB failure: {}", cleanupErr.getMessage());
        }
        mediaRepository.deleteMedia(mediaId);
        throw e;
      }

      // Generate thumbnail synchronously for images (file bytes already in memory)
      if (resolvedType == MediaType.IMAGE) {
        try {
          byte[] thumbnailBytes = thumbnailService.generate(file.getBytes());
          String thumbAssetId = UUID.randomUUID().toString();
          String thumbExtension = ".jpeg";
          s3Service.uploadAssetBytes(tenantId, mediaId, thumbAssetId, thumbExtension,
              thumbnailBytes, "image/jpeg", true);

          MediaAsset thumbAsset = MediaAsset.builder()
              .assetId(thumbAssetId)
              .mediaId(mediaId)
              .tenantId(tenantId)
              .sourceAssetId(assetId)
              .type(AssetType.THUMBNAIL)
              .tags(List.of("thumbnail"))
              .status(AssetStatus.COMPLETE)
              .outputFormat("jpeg")
              .mimetype("image/jpeg")
              .width(200)
              .size((long) thumbnailBytes.length)
              .downloadName(resolveDownloadName(fileName, AssetOperation.IMAGE_THUMBNAIL, "jpeg", null))
              .operation(AssetOperation.IMAGE_THUMBNAIL)
              .createdAt(Instant.now())
              .build();
          assetRepository.createAsset(thumbAsset);
        } catch (Exception e) {
          log.warn("Failed to generate sync thumbnail for media {}: {}", mediaId, e.getMessage());
          // Non-fatal: thumbnail will be generated async via createAssets if needed
        }
      }

      cacheInvalidationService.invalidatePaginationCache();

      uploadSuccessCounter.add(1);
      log.info("Media uploaded successfully: mediaId={}, assetId={}, fileName={}", mediaId, assetId, fileName);
      return media;
    } catch (Exception e) {
      uploadFailureCounter.add(1);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw e;
    } finally {
      span.end();
    }
  }

  public Optional<Media> getMedia(String mediaId) {
    return cacheOrchestrator.getMedia(mediaId);
  }

  public Optional<Media> getActiveMedia(String mediaId) {
    return getMedia(mediaId).map(media -> {
      requireActiveMediaAccess(media);
      return media;
    });
  }

  public MediaDynamoDbRepository.MediaPagedResult getMediaPaginated(String cursor, Integer limit, MediaType mediaType) {
    return mediaRepository.getMediaPaginated(cursor, limit, mediaType);
  }

  public MediaDynamoDbRepository.MediaPagedResult getMediaPaginated(String cursor, Integer limit, MediaType mediaType,
      MediaSource source) {
    return mediaRepository.getMediaPaginated(cursor, limit, mediaType, source);
  }

  public List<MediaAsset> listAssets(String mediaId) {
    getActiveMedia(mediaId);
    return assetRepository.listAssets(mediaId);
  }

  public List<MediaAsset> listAssetsPublic(String mediaId) {
    return assetRepository.listAssets(mediaId);
  }

  public Optional<MediaAsset> getAsset(String mediaId, String assetId) {
    return assetRepository.getAsset(mediaId, assetId);
  }

  public Optional<String> getAssetDownloadUrl(String mediaId, String assetId) {
    return findAuthorizedDownloadableAssetContext(mediaId, assetId)
        .map(this::buildDownloadUrlForAuthorizedRequest);
  }

  public Optional<String> getAssetPreviewUrl(String mediaId, String assetId) {
    return findAuthorizedDownloadableAssetContext(mediaId, assetId)
        .map(ctx -> buildAssetPreviewUrl(ctx.media, ctx.asset));
  }

  public Optional<String> getAssetDownloadUrlPublic(String mediaId, String assetId) {
    return findPublicDownloadableAssetContext(mediaId, assetId)
        .map(ctx -> buildAssetDownloadUrl(ctx.media, ctx.asset));
  }

  public Map<String, String> getThumbnailUrls(List<Media> mediaItems) {
    Map<String, String> urls = new HashMap<>();
    for (Media media : mediaItems) {
      if (media.getMediaType() != MediaType.IMAGE) continue;
      if (media.getStatus() == MediaStatus.DELETED) continue;

      var assets = assetRepository.listAssets(media.getMediaId());
      var thumbnail = assets.stream()
          .filter(a -> a.getOperation() == AssetOperation.IMAGE_THUMBNAIL
              && a.getStatus() == AssetStatus.COMPLETE)
          .findFirst();

      thumbnail.ifPresent(asset -> {
        try {
          String url = buildAssetDownloadUrl(media, asset);
          urls.put(media.getMediaId(), url);
        } catch (Exception e) {
          log.warn("Failed to generate thumbnail URL for media {}: {}", media.getMediaId(), e.getMessage());
        }
      });
    }
    return urls;
  }

  public List<MediaAsset> createAssets(String mediaId, CreateAssetRequest request) {
    var media = mediaRepository.getMedia(mediaId)
        .orElseThrow(() -> new IllegalArgumentException("Media not found"));
    requireActiveMediaAccess(media);

    String sourceAssetId = request.getSourceAssetId() != null ? request.getSourceAssetId() : media.getOriginalAssetId();
    var sourceAsset = assetRepository.getAsset(mediaId, sourceAssetId)
        .orElseThrow(() -> new IllegalArgumentException("Source asset not found"));
    if (sourceAsset.getStatus() != AssetStatus.COMPLETE) {
      throw new IllegalArgumentException("Source asset is not ready");
    }

    var existingAssets = new ArrayList<>(assetRepository.listAssets(mediaId));
    List<MediaAsset> created = new ArrayList<>();
    boolean createdNew = false;
    for (CreateAssetOutput output : request.getOutputs()) {
      AssetOperation operation = output.getOperation();
      validateOperation(media.getMediaType(), operation);

      String assetId = UUID.randomUUID().toString();
      String outputFormat = resolveOutputFormat(operation, output.getOutputFormat());
      Integer width = resolveWidth(operation, output.getWidth());
      AssetType type = resolveAssetType(operation);
      List<String> tags = resolveTags(operation, output.getTags());
      String downloadName = resolveDownloadName(media.getName(), operation, outputFormat, output.getDownloadName());

      MediaAsset existing = findMatchingAsset(existingAssets, sourceAssetId, operation, outputFormat, width, tags);
      if (existing != null && existing.getStatus() != AssetStatus.DELETED) {
        created.add(existing);
        continue;
      }

      MediaAsset asset = MediaAsset.builder()
          .assetId(assetId)
          .mediaId(mediaId)
          .tenantId(media.getTenantId())
          .sourceAssetId(sourceAssetId)
          .type(type)
          .tags(tags)
          .status(AssetStatus.PENDING)
          .outputFormat(outputFormat)
          .mimetype(resolveMimeType(operation, outputFormat))
          .width(width)
          .downloadName(downloadName)
          .operation(operation)
          .createdAt(Instant.now())
          .build();

      assetRepository.createAsset(asset);
      existingAssets.add(asset);
      createdNew = true;

      createAndPublishProcessingJob(media, asset);
      created.add(asset);
    }

    // Auto-create thumbnail for image media if one doesn't exist
    if (media.getMediaType() == MediaType.IMAGE && createdNew) {
      boolean hasThumbnail = existingAssets.stream()
          .anyMatch(a -> a.getOperation() == AssetOperation.IMAGE_THUMBNAIL
              && a.getStatus() != AssetStatus.DELETED);
      if (!hasThumbnail) {
        String thumbAssetId = UUID.randomUUID().toString();
        MediaAsset thumbAsset = MediaAsset.builder()
            .assetId(thumbAssetId)
            .mediaId(mediaId)
            .tenantId(media.getTenantId())
            .sourceAssetId(sourceAssetId)
            .type(AssetType.THUMBNAIL)
            .tags(List.of("thumbnail"))
            .status(AssetStatus.PENDING)
            .outputFormat("jpeg")
            .mimetype("image/jpeg")
            .width(200)
            .downloadName(resolveDownloadName(media.getName(), AssetOperation.IMAGE_THUMBNAIL, "jpeg", null))
            .operation(AssetOperation.IMAGE_THUMBNAIL)
            .createdAt(Instant.now())
            .build();
        assetRepository.createAsset(thumbAsset);
        createAndPublishProcessingJob(media, thumbAsset);
      }
    }

    if (createdNew) {
      mediaRepository.updateStatus(mediaId, MediaStatus.PROCESSING);
      cacheInvalidationService.invalidateMedia(mediaId);
    }
    return created;
  }

  public Optional<MediaAsset> retryAsset(String mediaId, String assetId) {
    var media = mediaRepository.getMedia(mediaId).orElse(null);
    if (media == null) {
      return Optional.empty();
    }
    requireActiveMediaAccess(media);
    var assetOpt = assetRepository.getAsset(mediaId, assetId);
    if (assetOpt.isEmpty()) {
      return Optional.empty();
    }
    var asset = assetOpt.get();
    if (asset.getStatus() != AssetStatus.ERROR) {
      throw new IllegalArgumentException("Asset must be in ERROR status to retry.");
    }

    boolean updated = assetRepository.updateStatusConditionally(mediaId, assetId, AssetStatus.PENDING, AssetStatus.ERROR);
    if (!updated) {
      return Optional.empty();
    }

    createAndPublishProcessingJob(media, asset);
    mediaRepository.updateStatus(mediaId, MediaStatus.PROCESSING);
    cacheInvalidationService.invalidateMedia(mediaId);
    return Optional.of(asset);
  }

  public Optional<Media> deleteMedia(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .filter(media -> media.getStatus() != MediaStatus.DELETED)
        .map(media -> {
          authorizationService.requireMediaAccess(media);
          mediaRepository.softDelete(mediaId, Duration.ofDays(mediaProperties.getSoftDeleteRetentionDays()));
          String tenantId = resolveTenantId(media);
          eventPublisher.publishDeleteMediaEvent(mediaId, tenantId, resolveMediaType(media).getValue());
          cacheInvalidationService.invalidateMedia(mediaId);
          return mediaRepository.getMedia(mediaId).orElse(media);
        });
  }

  public InitUploadResponse initPresignedUpload(InitUploadRequest request) {
    MediaType resolvedType = mediaTypeResolver.resolve(request.getMediaType(), request.getContentType(),
        request.getFileName());
    if (resolvedType == null) {
      throw new IllegalArgumentException(INVALID_FILE_TYPE_MESSAGE);
    }

    String mediaId = UUID.randomUUID().toString();
    String assetId = UUID.randomUUID().toString();
    String tenantId = TenantContext.getTenantId();
    String userId = TenantContext.getUserId();

    Media media = Media.builder()
        .mediaId(mediaId)
        .tenantId(tenantId)
        .userId(userId)
        .size(request.getFileSize())
        .name(request.getFileName())
        .mimetype(request.getContentType())
        .mediaType(resolvedType)
        .source(MediaSource.UPLOAD)
        .status(MediaStatus.PENDING_UPLOAD)
        .originalAssetId(assetId)
        .webhookUrl(request.getWebhookUrl())
        .createdAt(Instant.now())
        .build();

    mediaRepository.createMedia(media, Duration.ofHours(mediaProperties.getUpload().getPendingUploadTtlHours()));
    assetRepository.createAsset(buildOriginalAsset(
        assetId,
        mediaId,
        tenantId,
        request.getFileName(),
        request.getContentType(),
        request.getFileSize(),
        AssetStatus.PENDING_UPLOAD));
    cacheInvalidationService.invalidatePaginationCache();

    String uploadUrl = s3Service.generatePresignedUploadUrl(
        tenantId,
        mediaId,
        assetId,
        request.getFileName(),
        request.getContentType(),
        Duration.ofSeconds(mediaProperties.getUpload().getPresignedUrlExpirationSeconds()));

    return buildInitUploadResponse(mediaId, assetId, uploadUrl);
  }

  public Optional<InitUploadResponse> refreshPresignedUploadUrl(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .filter(media -> media.getStatus() == MediaStatus.PENDING_UPLOAD)
        .map(media -> {
          String tenantId = resolveTenantId(media);
          String assetId = media.getOriginalAssetId();
          String uploadUrl = s3Service.generatePresignedUploadUrl(
              tenantId,
              media.getMediaId(),
              assetId,
              media.getName(),
              media.getMimetype(),
              Duration.ofSeconds(mediaProperties.getUpload().getPresignedUrlExpirationSeconds()));
          return buildInitUploadResponse(mediaId, assetId, uploadUrl);
        });
  }

  public Optional<Media> completePresignedUpload(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .filter(media -> media.getStatus() == MediaStatus.PENDING_UPLOAD)
        .map(media -> {
          String tenantId = resolveTenantId(media);
          String assetId = media.getOriginalAssetId();
          boolean exists = s3Service.assetExists(tenantId, media.getMediaId(), assetId, media.getName());
          if (!exists) {
            throw new IllegalArgumentException("Upload not found in S3.");
          }
          mediaRepository.updateStatus(mediaId, MediaStatus.COMPLETE);
          assetRepository.updateStatus(mediaId, assetId, AssetStatus.COMPLETE);
          cacheInvalidationService.invalidateMedia(mediaId);
          return mediaRepository.getMedia(mediaId).orElse(media);
        });
  }

  public void validateUploadFile(long fileSize, boolean isEmpty) {
    if (isEmpty || fileSize <= 0) {
      throw new IllegalArgumentException("File is empty");
    }
    if (fileSize > mediaProperties.getMaxFileSize()) {
      throw new IllegalArgumentException("File size exceeds maximum allowed");
    }
  }

  public void validatePresignedUploadRequest(long fileSize, String contentType, MediaType mediaType, String fileName) {
    if (fileSize <= 0 || fileSize > mediaProperties.getUpload().getMaxPresignedUploadSize()) {
      throw new IllegalArgumentException("Invalid file size for presigned upload");
    }
    MediaType resolvedType = mediaTypeResolver.resolve(mediaType, contentType, fileName);
    if (resolvedType == null) {
      throw new IllegalArgumentException(INVALID_FILE_TYPE_MESSAGE);
    }
  }

  private InitUploadResponse buildInitUploadResponse(String mediaId, String assetId, String uploadUrl) {
    return InitUploadResponse.builder()
        .mediaId(mediaId)
        .assetId(assetId)
        .uploadUrl(uploadUrl)
        .expiresIn(mediaProperties.getUpload().getPresignedUrlExpirationSeconds())
        .method(PRESIGNED_UPLOAD_METHOD)
        .headers(Collections.emptyMap())
        .build();
  }

  private MediaAsset buildOriginalAsset(String assetId, String mediaId, String tenantId, String fileName,
      String contentType, long size, AssetStatus status) {
    return MediaAsset.builder()
        .assetId(assetId)
        .mediaId(mediaId)
        .tenantId(tenantId)
        .sourceAssetId(null)
        .type(AssetType.ORIGINAL)
        .tags(ORIGINAL_ASSET_TAGS)
        .status(status)
        .outputFormat(formatFromFileName(fileName))
        .mimetype(contentType)
        .size(size)
        .downloadName(fileName)
        .createdAt(Instant.now())
        .build();
  }

  private void createAndPublishProcessingJob(Media media, MediaAsset asset) {
    ProcessingJob job = buildProcessingJob(media, asset);
    jobRepository.createJob(job);
    eventPublisher.publishProcessingJob(job, media.getMediaType().getValue());
  }

  private ProcessingJob buildProcessingJob(Media media, MediaAsset asset) {
    return ProcessingJob.builder()
        .jobId(UUID.randomUUID().toString())
        .mediaId(media.getMediaId())
        .tenantId(media.getTenantId())
        .assetId(asset.getAssetId())
        .sourceAssetId(asset.getSourceAssetId())
        .operation(asset.getOperation())
        .outputFormat(asset.getOutputFormat())
        .width(asset.getWidth())
        .downloadName(asset.getDownloadName())
        .tags(asset.getTags())
        .status(ProcessingJobStatus.PENDING)
        .attempts(0)
        .createdAt(Instant.now())
        .build();
  }

  private Optional<AssetContext> findAssetContext(String mediaId, String assetId) {
    return assetRepository.getAsset(mediaId, assetId)
        .flatMap(asset -> mediaRepository.getMedia(mediaId).map(media -> new AssetContext(media, asset)));
  }

  private Optional<AssetContext> findAuthorizedDownloadableAssetContext(String mediaId, String assetId) {
    return findAssetContext(mediaId, assetId).flatMap(ctx -> {
      ensureMediaNotDeleted(ctx.media);
      authorizationService.requireMediaAccess(ctx.media);
      return asCompleteAssetContext(ctx);
    });
  }

  private Optional<AssetContext> findPublicDownloadableAssetContext(String mediaId, String assetId) {
    return findAssetContext(mediaId, assetId).flatMap(ctx -> {
      ensureMediaNotDeleted(ctx.media);
      return asCompleteAssetContext(ctx);
    });
  }

  private void requireActiveMediaAccess(Media media) {
    ensureMediaNotDeleted(media);
    authorizationService.requireMediaAccess(media);
  }

  private String buildDownloadUrlForAuthorizedRequest(AssetContext ctx) {
    if (!shouldRecordAnalytics(ctx.asset)) {
      return buildAssetDownloadUrl(ctx.media, ctx.asset);
    }
    return buildTrackedAssetDownloadUrl(ctx.media, ctx.asset);
  }

  private Optional<AssetContext> asCompleteAssetContext(AssetContext ctx) {
    if (ctx.asset.getStatus() != AssetStatus.COMPLETE) {
      return Optional.empty();
    }
    return Optional.of(ctx);
  }

  private void ensureMediaNotDeleted(Media media) {
    if (media.getStatus() == MediaStatus.DELETED) {
      throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
    }
  }

  private String resolveTenantId(Media media) {
    return media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
  }

  private MediaType resolveMediaType(Media media) {
    if (media.getMediaType() != null) {
      return media.getMediaType();
    }
    // Default to IMAGE for legacy records without mediaType
    return MediaType.IMAGE;
  }

  private void validateOperation(MediaType mediaType, AssetOperation operation) {
    if (mediaType == null) {
      throw new IllegalArgumentException("Media type is required for asset operations.");
    }
    if (operation == null) {
      throw new IllegalArgumentException("operation is required");
    }
    if (mediaType == MediaType.IMAGE && (operation == AssetOperation.DOCUMENT_PREVIEW || operation == AssetOperation.DOCUMENT_TEXT)) {
      throw new IllegalArgumentException(
          String.format("Operation '%s' is not supported for media type '%s'", operation, mediaType.getValue()));
    }
    if (mediaType == MediaType.DOCUMENT && (operation == AssetOperation.IMAGE_PROCESS || operation == AssetOperation.IMAGE_THUMBNAIL)) {
      throw new IllegalArgumentException(
          String.format("Operation '%s' is not supported for media type '%s'", operation, mediaType.getValue()));
    }
    if (mediaType != MediaType.IMAGE && mediaType != MediaType.DOCUMENT) {
      throw new IllegalArgumentException(
          String.format("Operation '%s' is not supported for media type '%s'", operation, mediaType.getValue()));
    }
  }

  private String resolveOutputFormat(AssetOperation operation, String requested) {
    return switch (operation) {
      case IMAGE_PROCESS, IMAGE_THUMBNAIL -> (requested != null && !requested.isBlank()) ? requested : "jpeg";
      case DOCUMENT_PREVIEW -> "png";
      case DOCUMENT_TEXT -> "json";
    };
  }

  private Integer resolveWidth(AssetOperation operation, Integer requested) {
    if (operation == AssetOperation.IMAGE_PROCESS || operation == AssetOperation.IMAGE_THUMBNAIL) {
      return mediaProperties.resolveWidth(requested);
    }
    return null;
  }

  private AssetType resolveAssetType(AssetOperation operation) {
    return switch (operation) {
      case IMAGE_THUMBNAIL, DOCUMENT_PREVIEW -> AssetType.THUMBNAIL;
      case DOCUMENT_TEXT -> AssetType.TEXT;
      case IMAGE_PROCESS -> AssetType.DERIVED;
    };
  }

  private List<String> resolveTags(AssetOperation operation, List<String> requested) {
    if (requested != null && !requested.isEmpty()) {
      return requested;
    }
    return switch (operation) {
      case IMAGE_THUMBNAIL -> List.of("thumbnail");
      case DOCUMENT_PREVIEW -> List.of("preview");
      case DOCUMENT_TEXT -> List.of("text");
      case IMAGE_PROCESS -> List.of("download");
    };
  }

  private String resolveDownloadName(String originalName, AssetOperation operation, String outputFormat, String requested) {
    if (requested != null && !requested.isBlank()) {
      return requested;
    }
    String base = (originalName != null && !originalName.isBlank())
        ? originalName.replaceAll("\\.[^.]+$", "")
        : "media";
    String suffix = switch (operation) {
      case IMAGE_THUMBNAIL -> "thumbnail";
      case DOCUMENT_PREVIEW -> "preview";
      case DOCUMENT_TEXT -> "text";
      case IMAGE_PROCESS -> "processed";
    };
    return base + "-" + suffix + "." + outputFormat;
  }

  private String resolveMimeType(AssetOperation operation, String outputFormat) {
    if (operation == AssetOperation.DOCUMENT_TEXT) {
      return "application/json";
    }
    if ("png".equalsIgnoreCase(outputFormat)) {
      return "image/png";
    }
    if ("webp".equalsIgnoreCase(outputFormat)) {
      return "image/webp";
    }
    if ("jpeg".equalsIgnoreCase(outputFormat) || "jpg".equalsIgnoreCase(outputFormat)) {
      return "image/jpeg";
    }
    return "application/octet-stream";
  }

  private MediaAsset findMatchingAsset(List<MediaAsset> assets, String sourceAssetId, AssetOperation operation,
      String outputFormat, Integer width, List<String> tags) {
    for (MediaAsset asset : assets) {
      if (asset == null || asset.getStatus() == AssetStatus.DELETED) {
        continue;
      }
      if (asset.getOperation() != operation) {
        continue;
      }
      if (sourceAssetId != null && !sourceAssetId.equals(asset.getSourceAssetId())) {
        continue;
      }
      if (outputFormat != null && asset.getOutputFormat() != null) {
        if (!outputFormat.equalsIgnoreCase(asset.getOutputFormat())) {
          continue;
        }
      } else if (outputFormat != null || asset.getOutputFormat() != null) {
        continue;
      }
      if (width != null && !width.equals(asset.getWidth())) {
        continue;
      }
      if (!tagsMatch(asset.getTags(), tags)) {
        continue;
      }
      return asset;
    }
    return null;
  }

  private boolean tagsMatch(List<String> existing, List<String> requested) {
    if (requested == null || requested.isEmpty()) {
      return true;
    }
    if (existing == null || existing.isEmpty()) {
      return false;
    }
    var normalizedExisting = existing.stream().map(String::toLowerCase).sorted().toList();
    var normalizedRequested = requested.stream().map(String::toLowerCase).sorted().toList();
    return normalizedExisting.equals(normalizedRequested);
  }

  private String formatFromFileName(String fileName) {
    String extension = StorageConstants.getFileExtension(fileName);
    if (extension.isEmpty()) {
      return null;
    }
    return extension.substring(1).toLowerCase();
  }

  private String extensionFromFormat(String format) {
    if (format == null || format.isBlank()) {
      return "";
    }
    return "." + format.toLowerCase();
  }

  private OutputFormat parseImageFormat(String value) {
    if (value == null) {
      return null;
    }
    for (OutputFormat format : OutputFormat.values()) {
      if (format.getFormat().equalsIgnoreCase(value)) {
        return format;
      }
    }
    return null;
  }

  private String buildAssetDownloadUrl(Media media, MediaAsset asset) {
    String tenantId = resolveTenantId(media);
    String extension = extensionFromFormat(asset.getOutputFormat());
    return s3Service.getAssetPresignedUrl(
        tenantId,
        media.getMediaId(),
        asset.getAssetId(),
        extension,
        asset.getDownloadName(),
        asset.getMimetype());
  }

  private String buildAssetPreviewUrl(Media media, MediaAsset asset) {
    String tenantId = resolveTenantId(media);
    String extension = extensionFromFormat(asset.getOutputFormat());
    return s3Service.getAssetPreviewPresignedUrl(
        tenantId,
        media.getMediaId(),
        asset.getAssetId(),
        extension,
        asset.getMimetype());
  }

  private String buildTrackedAssetDownloadUrl(Media media, MediaAsset asset) {
    String url = buildAssetDownloadUrl(media, asset);
    String tenantId = resolveTenantId(media);
    analyticsService.recordView(tenantId, media.getMediaId());
    OutputFormat format = parseImageFormat(asset.getOutputFormat());
    if (format != null && media.getMediaType() == MediaType.IMAGE) {
      analyticsService.recordDownload(tenantId, media.getMediaId(), format, asset.getWidth());
    }
    return url;
  }

  private boolean shouldRecordAnalytics(MediaAsset asset) {
    if (asset == null || asset.getType() == null) {
      return false;
    }
    return asset.getType() == AssetType.ORIGINAL || asset.getType() == AssetType.DERIVED;
  }

  private record AssetContext(Media media, MediaAsset asset) {
  }
}
