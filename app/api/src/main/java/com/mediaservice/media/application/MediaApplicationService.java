package com.mediaservice.media.application;

import com.mediaservice.shared.config.properties.MediaProperties;
import com.mediaservice.media.api.dto.InitUploadRequest;
import com.mediaservice.media.api.dto.InitUploadResponse;
import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.media.domain.service.ImageValidationService;
import com.mediaservice.media.domain.service.DocumentValidationService;
import com.mediaservice.media.domain.service.MediaTypeResolver;
import com.mediaservice.media.infrastructure.messaging.MediaEventPublisher;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import com.mediaservice.media.infrastructure.storage.S3StorageService;
import com.mediaservice.shared.cache.CacheInvalidationService;
import com.mediaservice.shared.cache.MultiLevelCacheOrchestrator;
import com.mediaservice.shared.config.properties.MediaProperties.Upload;
import com.mediaservice.shared.auth.AuthorizationService;
import com.mediaservice.shared.auth.TenantContext;
import com.mediaservice.shared.http.error.MediaGoneException;
import com.mediaservice.analytics.application.AnalyticsService;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Application service for media operations.
 *
 * <p>
 * This service orchestrates the media upload, processing, and retrieval flows.
 * It acts as the entry point for all media-related use cases.
 */
@Slf4j
@Service
public class MediaApplicationService {
  private final MediaDynamoDbRepository mediaRepository;
  private final S3StorageService s3Service;
  private final MediaEventPublisher eventPublisher;
  private final MediaProperties mediaProperties;
  private final ImageValidationService imageValidationService;
  private final DocumentValidationService documentValidationService;
  private final MediaTypeResolver mediaTypeResolver;
  private final CacheInvalidationService cacheInvalidationService;
  private final MultiLevelCacheOrchestrator cacheOrchestrator;
  private final AnalyticsService analyticsService;
  private final AuthorizationService authorizationService;
  private final Tracer tracer;
  private final LongCounter uploadSuccessCounter;
  private final LongCounter uploadFailureCounter;

  @org.springframework.beans.factory.annotation.Value("${aws.cloudfront.domain:}")
  private String cloudfrontDomain;

  @org.springframework.beans.factory.annotation.Value("${aws.cloudfront.enabled:false}")
  private boolean cloudfrontEnabled;

  public MediaApplicationService(MediaDynamoDbRepository mediaRepository, S3StorageService s3Service,
      MediaEventPublisher eventPublisher, MediaProperties mediaProperties,
      ImageValidationService imageValidationService, DocumentValidationService documentValidationService,
      MediaTypeResolver mediaTypeResolver, CacheInvalidationService cacheInvalidationService,
      MultiLevelCacheOrchestrator cacheOrchestrator, AnalyticsService analyticsService,
      AuthorizationService authorizationService,
      Tracer tracer, Meter meter) {
    this.mediaRepository = mediaRepository;
    this.s3Service = s3Service;
    this.eventPublisher = eventPublisher;
    this.mediaProperties = mediaProperties;
    this.imageValidationService = imageValidationService;
    this.documentValidationService = documentValidationService;
    this.mediaTypeResolver = mediaTypeResolver;
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

  public MediaResponse uploadMedia(MultipartFile file, Integer width, String outputFormat, String mediaType) throws IOException {
    String contentType = file.getContentType();
    MediaType resolvedType = mediaTypeResolver.resolve(MediaType.fromString(mediaType), contentType, file.getOriginalFilename());
    if (resolvedType == null) {
      throw new IllegalArgumentException("Invalid file type. Only images and PDFs are supported.");
    }

    if (resolvedType == MediaType.IMAGE) {
      // Validate actual image content (magic bytes + parsing)
      imageValidationService.validateImage(file);
    } else if (resolvedType == MediaType.DOCUMENT) {
      documentValidationService.validatePdf(file);
    }

    Span span = tracer.spanBuilder("upload-media-file")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      Integer targetWidth = null;
      OutputFormat targetFormat = null;
      if (resolvedType == MediaType.IMAGE) {
        targetWidth = mediaProperties.resolveWidth(width);
        targetFormat = OutputFormat.fromString(outputFormat);
      }

      String originalName = file.getOriginalFilename();
      String fileName = (originalName == null || originalName.isEmpty()) ? "image.jpg" : originalName;
      span.setAttribute("file.name", fileName);
      if (targetFormat != null) {
        span.setAttribute("output.format", targetFormat.getFormat());
      }
      span.setAttribute("media.type", resolvedType.getValue());
      String tenantId = TenantContext.getTenantId();
      String userId = TenantContext.getUserId();

      // Step 1: Upload original to S3
      s3Service.uploadMedia(tenantId, mediaId, fileName, file);
      // Step 2: Store metadata in DynamoDB with PENDING status
      try {
        mediaRepository.createMedia(Media.builder()
            .mediaId(mediaId)
            .tenantId(tenantId)
            .userId(userId)
            .size(file.getSize())
            .name(fileName)
            .mimetype(contentType)
            .mediaType(resolvedType)
            .status(resolvedType == MediaType.DOCUMENT ? MediaStatus.COMPLETE : MediaStatus.PENDING)
            .width(targetWidth)
            .outputFormat(targetFormat)
            .build());
      } catch (Exception e) {
        // Compensate: delete S3 object
        compensateS3Upload(tenantId, mediaId, fileName);
        throw e;
      }
      // Step 3: Publish event to SNS for async processing by Lambda (images only for now)
      if (resolvedType == MediaType.IMAGE) {
        try {
          eventPublisher.publishProcessMediaEvent(mediaId, tenantId, resolvedType.getValue(), targetWidth,
              targetFormat != null ? targetFormat.getFormat() : null);
        } catch (Exception e) {
          // Compensate: delete DynamoDB record and S3 object
          compensateDynamoDb(mediaId);
          compensateS3Upload(tenantId, mediaId, fileName);
          throw e;
        }
      }
      span.setStatus(StatusCode.OK);
      uploadSuccessCounter.add(1);
      log.info("Media uploaded successfully: mediaId={}, fileName={}, mediaType={}, outputFormat={}",
          mediaId, fileName, resolvedType.getValue(),
          targetFormat != null ? targetFormat.getFormat() : "n/a");
      return MediaResponse.builder().mediaId(mediaId).build();
    } catch (Exception e) {
      uploadFailureCounter.add(1);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw e;
    } finally {
      span.end();
    }
  }

  private void compensateS3Upload(String tenantId, String mediaId, String fileName) {
    try {
      s3Service.deleteUpload(tenantId, mediaId, fileName);
    } catch (Exception e) {
      log.error("Failed to compensate S3 upload for mediaId={}: {}", mediaId, e.getMessage());
    }
  }

  private void compensateDynamoDb(String mediaId) {
    try {
      mediaRepository.deleteMedia(mediaId);
    } catch (Exception e) {
      log.error("Failed to compensate DynamoDB record for mediaId={}: {}", mediaId, e.getMessage());
    }
  }

  /**
   * Get media status.
   * Note: Status is NOT cached because it changes frequently during processing
   * and caching would cause stale status to be returned during polling.
   */
  public Optional<MediaStatus> getMediaStatus(String mediaId) {
    return mediaRepository.getMedia(mediaId).map(Media::getStatus);
  }

  /**
   * Get media details with multi-level caching.
   */
  public Optional<Media> getMedia(String mediaId) {
    return cacheOrchestrator.getMedia(mediaId);
  }

  /**
   * Get media, throwing MediaGoneException if deleted.
   * Use this for API endpoints that should return 410 for deleted media.
   *
   * @param mediaId the media ID
   * @return the media if found and not deleted, empty if not found
   * @throws MediaGoneException if media is deleted
   */
  public Optional<Media> getActiveMedia(String mediaId) {
    return getMedia(mediaId).map(media -> {
      if (media.getStatus() == MediaStatus.DELETED) {
        throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
      }
      authorizationService.requireMediaAccess(media);
      return media;
    });
  }

  /**
   * Get media status, throwing MediaGoneException if deleted.
   *
   * @param mediaId the media ID
   * @return the status if found and not deleted, empty if not found
   * @throws MediaGoneException if media is deleted
   */
  public Optional<MediaStatus> getActiveMediaStatus(String mediaId) {
    return mediaRepository.getMedia(mediaId).map(media -> {
      if (media.getStatus() == MediaStatus.DELETED) {
        throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
      }
      authorizationService.requireMediaAccess(media);
      return media.getStatus();
    });
  }

  /**
   * Prepare download for media.
   * Handles all business logic: not found, deleted, processing, and ready states.
   * Records analytics (view + download) when download is ready.
   *
   * @param mediaId the media ID
   * @return DownloadResult indicating the state and data
   * @throws MediaGoneException if media is deleted
   */
  public DownloadResult prepareDownload(String mediaId) {
    var mediaOpt = mediaRepository.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return new DownloadResult.NotFound();
    }

    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
    }
    authorizationService.requireMediaAccess(media);

    if (media.getStatus() != MediaStatus.COMPLETE) {
      return new DownloadResult.Processing(mediaId);
    }

    return getDownloadUrl(media)
        .map(url -> {
          // Record analytics in application layer
          String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
          analyticsService.recordView(tenantId, mediaId);
          if (resolveMediaType(media) == MediaType.IMAGE) {
            analyticsService.recordDownload(tenantId, mediaId, media.getOutputFormatOrDefault(), media.getWidth());
          }
          return (DownloadResult) new DownloadResult.Ready(url, media);
        })
        .orElse(new DownloadResult.NotFound());
  }

  /**
   * Prepare download for public access (no tenant authorization enforced).
   */
  public DownloadResult prepareDownloadPublic(String mediaId) {
    var mediaOpt = mediaRepository.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return new DownloadResult.NotFound();
    }

    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
    }

    if (media.getStatus() != MediaStatus.COMPLETE) {
      return new DownloadResult.Processing(mediaId);
    }

    return getDownloadUrl(media)
        .map(url -> {
          String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
          analyticsService.recordView(tenantId, mediaId);
          if (resolveMediaType(media) == MediaType.IMAGE) {
            analyticsService.recordDownload(tenantId, mediaId, media.getOutputFormatOrDefault(), media.getWidth());
          }
          return (DownloadResult) new DownloadResult.Ready(url, media);
        })
        .orElse(new DownloadResult.NotFound());
  }

  /**
   * Prepare preview for media.
   * Handles all business logic: not found, processing, and ready states.
   * Records view analytics when preview is ready.
   *
   * @param mediaId the media ID
   * @return PreviewResult indicating the state and data
   */
  public PreviewResult preparePreview(String mediaId) {
    var mediaOpt = mediaRepository.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return new PreviewResult.NotFound();
    }

    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      return new PreviewResult.NotFound();
    }

    authorizationService.requireMediaAccess(media);

    if (resolveMediaType(media) != MediaType.IMAGE) {
      return new PreviewResult.NotFound();
    }

    if (media.getStatus() != MediaStatus.COMPLETE) {
      return new PreviewResult.Processing(mediaId);
    }

    return getPreviewUrl(media)
        .map(url -> {
          // Record view analytics in application layer
          String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
          analyticsService.recordView(tenantId, mediaId);
          return (PreviewResult) new PreviewResult.Ready(url);
        })
        .orElse(new PreviewResult.NotFound());
  }

  /**
   * Prepare preview for public access (no tenant authorization enforced).
   */
  public PreviewResult preparePreviewPublic(String mediaId) {
    var mediaOpt = mediaRepository.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return new PreviewResult.NotFound();
    }

    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
    }

    if (resolveMediaType(media) != MediaType.IMAGE) {
      return new PreviewResult.NotFound();
    }

    if (media.getStatus() != MediaStatus.COMPLETE) {
      return new PreviewResult.Processing(mediaId);
    }

    return getPreviewUrl(media)
        .map(url -> {
          String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
          analyticsService.recordView(tenantId, mediaId);
          return (PreviewResult) new PreviewResult.Ready(url);
        })
        .orElse(new PreviewResult.NotFound());
  }

  /**
   * Get presigned URL for the original uploaded file.
   *
   * @param mediaId the media ID
   * @return presigned URL if media exists and has been uploaded, empty otherwise
   * @throws MediaGoneException if media is deleted
   */
  public Optional<String> getOriginalUrl(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .map(media -> {
          if (media.getStatus() == MediaStatus.DELETED) {
            throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
          }
          authorizationService.requireMediaAccess(media);
          if (media.getStatus() == MediaStatus.PENDING_UPLOAD) {
            return null;
          }
          String tenantId = media.getTenantId() != null ? media.getTenantId() : "default";
          return s3Service.getOriginalPresignedUrl(tenantId, mediaId, media.getName());
        });
  }

  /**
   * Get presigned URL for the original uploaded file (public access).
   */
  public Optional<String> getOriginalUrlPublic(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .map(media -> {
          if (media.getStatus() == MediaStatus.DELETED) {
            throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
          }
          if (media.getStatus() == MediaStatus.PENDING_UPLOAD) {
            return null;
          }
          String tenantId = media.getTenantId() != null ? media.getTenantId() : "default";
          return s3Service.getOriginalPresignedUrl(tenantId, mediaId, media.getName());
        });
  }

  /**
   * Get presigned download URL with multi-level caching.
   */
  public Optional<String> getDownloadUrl(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .filter(media -> media.getStatus() == MediaStatus.COMPLETE)
        .flatMap(this::getDownloadUrl);
  }

  /**
   * Get preview URL for CDN-served watermarked preview image.
   * Returns CloudFront URL if enabled, otherwise falls back to S3 presigned URL.
   */
  public Optional<String> getPreviewUrl(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .filter(media -> media.getStatus() == MediaStatus.COMPLETE)
        .flatMap(this::getPreviewUrl);
  }

  private Optional<String> getDownloadUrl(Media media) {
    String tenantId = media.getTenantId() != null ? media.getTenantId() : "default";
    if (resolveMediaType(media) == MediaType.DOCUMENT) {
      return Optional.ofNullable(s3Service.getOriginalPresignedUrl(tenantId, media.getMediaId(), media.getName()));
    }
    var format = media.getOutputFormatOrDefault();
    return cacheOrchestrator.getPresignedUrl(
        media.getMediaId(),
        format.getFormat(),
        () -> s3Service.getPresignedUrl(tenantId, media.getMediaId(), media.getName(), format));
  }

  private Optional<String> getPreviewUrl(Media media) {
    if (resolveMediaType(media) != MediaType.IMAGE) {
      return Optional.empty();
    }
    return Optional.ofNullable(buildPreviewUrl(media));
  }

  private String buildPreviewUrl(Media media) {
    var format = media.getOutputFormatOrDefault();
    String tenantId = media.getTenantId() != null ? media.getTenantId() : "default";
    String key = StorageConstants.buildS3Key(tenantId, media.getMediaId(), StorageConstants.VARIANT_PREVIEW,
        format.getExtension());

    if (cloudfrontEnabled && cloudfrontDomain != null && !cloudfrontDomain.isEmpty()) {
      return "https://" + cloudfrontDomain + "/" + key;
    }
    // Fallback to S3 presigned URL if CloudFront not configured
    return s3Service.getPreviewPresignedUrl(tenantId, media.getMediaId(), format).orElse(null);
  }

  public boolean isMediaProcessing(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .map(media -> media.getStatus() != MediaStatus.COMPLETE)
        .orElse(false);
  }

  /**
   * Resize media to a new width/format.
   *
   * @param mediaId      the media ID
   * @param width        target width
   * @param outputFormat target format (optional)
   * @return MediaOperationResult indicating success or failure reason
   */
  public MediaOperationResult resizeMedia(String mediaId, Integer width, String outputFormat) {
    var mediaOpt = mediaRepository.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return new MediaOperationResult.NotFound(mediaId);
    }

    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      return new MediaOperationResult.Deleted(mediaId, media.getDeletedAt());
    }
    authorizationService.requireMediaAccess(media);
    if (resolveMediaType(media) != MediaType.IMAGE) {
      return new MediaOperationResult.NotAllowed(mediaId, "Resize is only supported for images.");
    }

    var updated = mediaRepository.updateStatusConditionally(mediaId, MediaStatus.PENDING, MediaStatus.COMPLETE);
    if (!updated) {
      log.warn("Cannot resize mediaId: {}, not in COMPLETE status (current: {})", mediaId, media.getStatus());
      return new MediaOperationResult.NotAllowed(mediaId,
          "Media must be in COMPLETE status to resize. Current status: " + media.getStatus());
    }

    OutputFormat targetFormat = outputFormat != null
        ? OutputFormat.fromString(outputFormat)
        : media.getOutputFormatOrDefault();
    String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
    try {
      eventPublisher.publishResizeMediaEvent(mediaId, tenantId, MediaType.IMAGE.getValue(), width, targetFormat.getFormat());
    } catch (Exception e) {
      log.error("Failed to publish resize event for mediaId={}, reverting status to COMPLETE", mediaId, e);
      mediaRepository.updateStatusConditionally(mediaId, MediaStatus.COMPLETE, MediaStatus.PENDING);
      throw e;
    }
    cacheInvalidationService.invalidateMedia(mediaId);
    log.info("Resize request submitted for mediaId: {} with outputFormat: {}", mediaId, targetFormat.getFormat());

    return new MediaOperationResult.Success(media);
  }

  /**
   * Soft delete media: marks as DELETED, publishes event for S3 cleanup.
   * The DynamoDB record is retained for analytics/audit purposes.
   */
  public Optional<Media> deleteMedia(String mediaId) {
    return mediaRepository.getMedia(mediaId)
        .filter(media -> media.getStatus() != MediaStatus.DELETED)
        .map(media -> {
          authorizationService.requireMediaAccess(media);
          mediaRepository.softDelete(mediaId, Duration.ofDays(mediaProperties.getSoftDeleteRetentionDays()));
          String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
          try {
            eventPublisher.publishDeleteMediaEvent(mediaId, tenantId, resolveMediaType(media).getValue());
          } catch (Exception e) {
            log.error("Failed to publish delete event for mediaId={}, reverting soft delete", mediaId, e);
            mediaRepository.revertSoftDelete(mediaId, media.getStatus());
            throw e;
          }
          cacheInvalidationService.invalidateMedia(mediaId);
          log.info("Soft delete completed for mediaId: {}", mediaId);
          return media;
        });
  }

  public boolean mediaExists(String mediaId) {
    return mediaRepository.getMedia(mediaId).isPresent();
  }

  /**
   * Retry processing for media stuck in PROCESSING or ERROR status.
   * Resets status to PENDING and re-publishes the process event.
   *
   * @param mediaId the media ID to retry
   * @return MediaOperationResult indicating success or failure reason
   */
  public MediaOperationResult retryProcessing(String mediaId) {
    var mediaOpt = mediaRepository.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return new MediaOperationResult.NotFound(mediaId);
    }

    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      return new MediaOperationResult.Deleted(mediaId, media.getDeletedAt());
    }

    authorizationService.requireMediaAccess(media);
    if (resolveMediaType(media) != MediaType.IMAGE) {
      return new MediaOperationResult.NotAllowed(mediaId, "Retry is only supported for image processing.");
    }

    if (media.getStatus() != MediaStatus.PROCESSING && media.getStatus() != MediaStatus.ERROR) {
      return new MediaOperationResult.NotAllowed(mediaId,
          "Retry only allowed for PROCESSING or ERROR status. Current status: " + media.getStatus());
    }

    boolean updated = mediaRepository.updateStatusConditionally(mediaId, MediaStatus.PENDING, media.getStatus());
    if (!updated) {
      log.warn("Failed to reset status for retry: mediaId={}", mediaId);
      return new MediaOperationResult.NotAllowed(mediaId, "Failed to update status (concurrent modification)");
    }

    String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
    try {
      eventPublisher.publishProcessMediaEvent(mediaId, tenantId, MediaType.IMAGE.getValue(),
          media.getWidth(), media.getOutputFormatOrDefault().getFormat());
    } catch (Exception e) {
      log.error("Failed to publish retry event for mediaId={}, reverting status to {}", mediaId, media.getStatus(), e);
      mediaRepository.updateStatusConditionally(mediaId, media.getStatus(), MediaStatus.PENDING);
      throw e;
    }
    cacheInvalidationService.invalidateMedia(mediaId);
    log.info("Retry initiated for mediaId={}, previousStatus={}", mediaId, media.getStatus());

    return new MediaOperationResult.Success(media);
  }

  public MediaDynamoDbRepository.MediaPagedResult getMediaPaginated(String cursor, Integer limit, MediaType mediaType) {
    if (TenantContext.isAuthenticated()) {
      return mediaRepository.getMediaPaginatedByTenant(TenantContext.getTenantId(), cursor, limit, mediaType);
    }
    return mediaRepository.getMediaPaginated(cursor, limit, mediaType);
  }

  public InitUploadResponse initPresignedUpload(InitUploadRequest request) {
    Span span = tracer.spanBuilder("init-presigned-upload")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      String tenantId = TenantContext.getTenantId();
      String userId = TenantContext.getUserId();

      MediaType resolvedType = mediaTypeResolver.resolve(request.getMediaType(),
          request.getContentType(), request.getFileName());
      if (resolvedType == null) {
        throw new IllegalArgumentException("Invalid file type. Only images and PDFs are supported.");
      }

      Integer targetWidth = null;
      OutputFormat targetFormat = null;
      if (resolvedType == MediaType.IMAGE) {
        targetWidth = mediaProperties.resolveWidth(request.getWidth());
        targetFormat = OutputFormat.fromString(request.getOutputFormat());
      }
      int expirationSeconds = mediaProperties.getUpload().getPresignedUrlExpirationSeconds();

      span.setAttribute("media.type", resolvedType.getValue());
      if (targetFormat != null) {
        span.setAttribute("output.format", targetFormat.getFormat());
      }

      String uploadUrl = s3Service.generatePresignedUploadUrl(
          tenantId,
          mediaId,
          request.getFileName(),
          request.getContentType(),
          Duration.ofSeconds(expirationSeconds));

      Duration ttl = Duration.ofHours(mediaProperties.getUpload().getPendingUploadTtlHours());
      mediaRepository.createMedia(Media.builder()
          .mediaId(mediaId)
          .tenantId(tenantId)
          .userId(userId)
          .size(request.getFileSize())
          .name(request.getFileName())
          .mimetype(request.getContentType())
          .mediaType(resolvedType)
          .status(MediaStatus.PENDING_UPLOAD)
          .width(targetWidth)
          .outputFormat(targetFormat)
          .webhookUrl(request.getWebhookUrl())
          .build(), ttl);

      var headers = new LinkedHashMap<String, String>();
      headers.put("Content-Type", request.getContentType());

      span.setStatus(StatusCode.OK);
      log.info("Presigned upload initialized: mediaId={}, fileName={}, outputFormat={}", mediaId, request.getFileName(),
          targetFormat != null ? targetFormat.getFormat() : "n/a");

      return InitUploadResponse.builder()
          .mediaId(mediaId)
          .uploadUrl(uploadUrl)
          .expiresIn(expirationSeconds)
          .method("PUT")
          .headers(headers)
          .build();
    } catch (Exception e) {
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw e;
    } finally {
      span.end();
    }
  }

  public Optional<InitUploadResponse> refreshPresignedUploadUrl(String mediaId) {
    Span span = tracer.spanBuilder("refresh-presigned-upload-url")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      span.setAttribute("media.id", mediaId);

      return mediaRepository.getMedia(mediaId)
          .filter(media -> media.getStatus() == MediaStatus.PENDING_UPLOAD)
          .map(media -> {
            int expirationSeconds = mediaProperties.getUpload().getPresignedUrlExpirationSeconds();
            String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();

            String uploadUrl = s3Service.generatePresignedUploadUrl(
                tenantId,
                mediaId,
                media.getName(),
                media.getMimetype(),
                Duration.ofSeconds(expirationSeconds));

            var headers = new LinkedHashMap<String, String>();
            headers.put("Content-Type", media.getMimetype());

            span.setStatus(StatusCode.OK);
            log.info("Presigned upload URL refreshed: mediaId={}", mediaId);

            return InitUploadResponse.builder()
                .mediaId(mediaId)
                .uploadUrl(uploadUrl)
                .expiresIn(expirationSeconds)
                .method("PUT")
                .headers(headers)
                .build();
          });
    } catch (Exception e) {
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw e;
    } finally {
      span.end();
    }
  }

  public Optional<Media> completePresignedUpload(String mediaId) {
    Span span = tracer.spanBuilder("complete-presigned-upload")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      span.setAttribute("media.id", mediaId);

      return mediaRepository.getMedia(mediaId)
          .filter(media -> media.getStatus() == MediaStatus.PENDING_UPLOAD)
          .flatMap(media -> {
            String tenantId = media.getTenantId() != null ? media.getTenantId() : TenantContext.getTenantId();
            MediaType resolvedType = resolveMediaType(media);
            if (!s3Service.objectExists(tenantId, mediaId, media.getName())) {
              log.warn("File not found in S3 for mediaId: {}", mediaId);
              return Optional.empty();
            }

            MediaStatus nextStatus = resolvedType == MediaType.DOCUMENT ? MediaStatus.COMPLETE : MediaStatus.PENDING;
            boolean updated = mediaRepository.updateStatusConditionally(
                mediaId, nextStatus, MediaStatus.PENDING_UPLOAD, true);
            if (!updated) {
              log.warn("Failed to update status for mediaId: {}", mediaId);
              return Optional.empty();
            }

            if (resolvedType == MediaType.IMAGE) {
              try {
                eventPublisher.publishProcessMediaEvent(mediaId, tenantId, resolvedType.getValue(),
                    media.getWidth(), media.getOutputFormatOrDefault().getFormat());
              } catch (Exception e) {
                log.error("Failed to publish process event for mediaId={}, reverting status to PENDING_UPLOAD", mediaId, e);
                mediaRepository.updateStatusConditionally(mediaId, MediaStatus.PENDING_UPLOAD, MediaStatus.PENDING);
                throw e;
              }
            }
            uploadSuccessCounter.add(1);
            span.setStatus(StatusCode.OK);
            log.info("Presigned upload completed: mediaId={}", mediaId);
            return Optional.of(media);
          });
    } catch (Exception e) {
      uploadFailureCounter.add(1);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw e;
    } finally {
      span.end();
    }
  }

  /**
   * Validate an upload file for size and content.
   *
   * @param fileSize the file size in bytes
   * @param isEmpty whether the file is empty
   * @throws IllegalArgumentException if validation fails
   */
  public void validateUploadFile(long fileSize, boolean isEmpty) {
    long maxFileSize = mediaProperties.getMaxFileSize();
    if (fileSize > maxFileSize) {
      throw new IllegalArgumentException(
          "Failed to upload media. Check the file size. Max size is " + (maxFileSize / (1024 * 1024)) + " MB.");
    }
    if (isEmpty) {
      throw new IllegalArgumentException("Malformed multipart form data.");
    }
  }

  /**
   * Validate a presigned upload request.
   *
   * @param fileSize the file size in bytes
   * @param contentType the content type
   * @throws IllegalArgumentException if validation fails
   */
  public void validatePresignedUploadRequest(long fileSize, String contentType, MediaType mediaType, String fileName) {
    Upload uploadConfig = mediaProperties.getUpload();
    long maxUploadSize = uploadConfig.getMaxPresignedUploadSize();
    if (fileSize > maxUploadSize) {
      throw new IllegalArgumentException(
          "File size exceeds maximum allowed size of " + (maxUploadSize / (1024 * 1024 * 1024)) + " GB.");
    }
    MediaType resolvedType = mediaTypeResolver.resolve(mediaType, contentType, fileName);
    if (resolvedType == null) {
      throw new IllegalArgumentException("Invalid content type. Only images and PDFs are supported.");
    }
  }

  private MediaType resolveMediaType(Media media) {
    return media.getMediaType() != null ? media.getMediaType() : MediaType.IMAGE;
  }
}
