package com.mediaservice.service;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.dto.InitUploadRequest;
import com.mediaservice.dto.InitUploadResponse;
import com.mediaservice.dto.MediaResponse;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
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

@Slf4j
@Service
public class MediaService {
  private final DynamoDbService dynamoDbService;
  private final S3Service s3Service;
  private final SnsService snsService;
  private final MediaProperties mediaProperties;
  private final ImageValidationService imageValidationService;
  private final CacheInvalidationService cacheInvalidationService;
  private final Tracer tracer;
  private final LongCounter uploadSuccessCounter;
  private final LongCounter uploadFailureCounter;

  public MediaService(DynamoDbService dynamoDbService, S3Service s3Service, SnsService snsService,
      MediaProperties mediaProperties, ImageValidationService imageValidationService,
      CacheInvalidationService cacheInvalidationService, Tracer tracer, Meter meter) {
    this.dynamoDbService = dynamoDbService;
    this.s3Service = s3Service;
    this.snsService = snsService;
    this.mediaProperties = mediaProperties;
    this.imageValidationService = imageValidationService;
    this.cacheInvalidationService = cacheInvalidationService;
    this.tracer = tracer;
    this.uploadSuccessCounter = meter.counterBuilder("media.upload.success")
        .setDescription("Count of successful media uploads")
        .build();
    this.uploadFailureCounter = meter.counterBuilder("media.upload.failure")
        .setDescription("Count of failed media uploads")
        .build();
  }

  public MediaResponse uploadMedia(MultipartFile file, Integer width, String outputFormat) throws IOException {
    String contentType = file.getContentType();
    if (contentType == null || !contentType.startsWith("image")) {
      throw new IllegalArgumentException("Invalid file type. Only images are supported.");
    }

    // Validate actual image content (magic bytes + parsing)
    imageValidationService.validateImage(file);

    Span span = tracer.spanBuilder("upload-media-file")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      int targetWidth = mediaProperties.resolveWidth(width);
      OutputFormat targetFormat = OutputFormat.fromString(outputFormat);

      String originalName = file.getOriginalFilename();
      String fileName = (originalName == null || originalName.isEmpty()) ? "image.jpg" : originalName;
      span.setAttribute("file.name", fileName);
      span.setAttribute("output.format", targetFormat.getFormat());
      // Step 1: Upload original to S3
      s3Service.uploadMedia(mediaId, fileName, file);
      // Step 2: Store metadata in DynamoDB with PENDING status
      try {
        dynamoDbService.createMedia(Media.builder()
            .mediaId(mediaId)
            .size(file.getSize())
            .name(fileName)
            .mimetype(contentType)
            .status(MediaStatus.PENDING)
            .width(targetWidth)
            .outputFormat(targetFormat)
            .build());
      } catch (Exception e) {
        // Compensate: delete S3 object
        compensateS3Upload(mediaId, fileName);
        throw e;
      }
      // Step 3: Publish event to SNS for async processing by Lambda
      try {
        snsService.publishProcessMediaEvent(mediaId, targetWidth, targetFormat.getFormat());
      } catch (Exception e) {
        // Compensate: delete DynamoDB record and S3 object
        compensateDynamoDb(mediaId);
        compensateS3Upload(mediaId, fileName);
        throw e;
      }
      span.setStatus(StatusCode.OK);
      uploadSuccessCounter.add(1);
      log.info("Media uploaded successfully: mediaId={}, fileName={}, outputFormat={}", mediaId, fileName,
          targetFormat.getFormat());
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

  private void compensateS3Upload(String mediaId, String fileName) {
    try {
      s3Service.deleteUpload(mediaId, fileName);
    } catch (Exception e) {
      log.error("Failed to compensate S3 upload for mediaId={}: {}", mediaId, e.getMessage());
    }
  }

  private void compensateDynamoDb(String mediaId) {
    try {
      dynamoDbService.deleteMedia(mediaId);
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
    return dynamoDbService.getMedia(mediaId).map(Media::getStatus);
  }

  /**
   * Get media details.
   * Note: Not cached because Optional doesn't serialize well with Redis JSON.
   * Consider using a wrapper DTO for caching if needed.
   */
  public Optional<Media> getMedia(String mediaId) {
    return dynamoDbService.getMedia(mediaId);
  }

  /**
   * Get presigned download URL.
   * Note: Not cached because Optional doesn't serialize well with Redis JSON.
   * The URL generation is fast and S3 presigned URLs have built-in expiry.
   */
  public Optional<String> getDownloadUrl(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .filter(media -> media.getStatus() == MediaStatus.COMPLETE)
        .map(media -> {
          var format = media.getOutputFormat() != null ? media.getOutputFormat() : OutputFormat.JPEG;
          return s3Service.getPresignedUrl(mediaId, media.getName(), format);
        });
  }

  public boolean isMediaProcessing(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> media.getStatus() != MediaStatus.COMPLETE)
        .orElse(false);
  }

  public Optional<Media> resizeMedia(String mediaId, Integer width, String outputFormat) {
    return dynamoDbService.getMedia(mediaId)
        .flatMap(media -> {
          var updated = dynamoDbService.updateStatusConditionally(mediaId, MediaStatus.PENDING, MediaStatus.COMPLETE);
          if (!updated) {
            log.warn("Cannot resize mediaId: {}, not in COMPLETE status", mediaId);
            return Optional.empty();
          }
          // Use provided outputFormat or fall back to media's existing format
          OutputFormat targetFormat = outputFormat != null
              ? OutputFormat.fromString(outputFormat)
              : (media.getOutputFormat() != null ? media.getOutputFormat() : OutputFormat.JPEG);
          snsService.publishResizeMediaEvent(mediaId, width, targetFormat.getFormat());

          // Invalidate caches for this media
          cacheInvalidationService.invalidateMedia(mediaId);

          log.info("Resize request submitted for mediaId: {} with outputFormat: {}", mediaId, targetFormat.getFormat());
          return Optional.of(media);
        });
  }

  public Optional<Media> deleteMedia(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> {
          dynamoDbService.updateStatus(mediaId, MediaStatus.DELETING);
          snsService.publishDeleteMediaEvent(mediaId);

          // Invalidate caches for this media
          cacheInvalidationService.invalidateMedia(mediaId);

          log.info("Delete request submitted for mediaId: {}", mediaId);
          return media;
        });
  }

  public boolean mediaExists(String mediaId) {
    return dynamoDbService.getMedia(mediaId).isPresent();
  }

  public DynamoDbService.PagedResult getMediaPaginated(String cursor, Integer limit) {
    return dynamoDbService.getMediaPaginated(cursor, limit);
  }

  @Deprecated
  public List<Media> getAllMedia() {
    return dynamoDbService.getAllMedia();
  }

  public InitUploadResponse initPresignedUpload(InitUploadRequest request) {
    Span span = tracer.spanBuilder("init-presigned-upload")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      int targetWidth = mediaProperties.resolveWidth(request.getWidth());
      OutputFormat targetFormat = OutputFormat.fromString(request.getOutputFormat());
      int expirationSeconds = mediaProperties.getUpload().getPresignedUrlExpirationSeconds();

      span.setAttribute("output.format", targetFormat.getFormat());

      // Generate presigned URL
      String uploadUrl = s3Service.generatePresignedUploadUrl(
          mediaId,
          request.getFileName(),
          request.getContentType(),
          Duration.ofSeconds(expirationSeconds));

      // Store metadata in DynamoDB with PENDING_UPLOAD status and TTL
      // TTL ensures stale records are automatically cleaned up if client never calls
      // complete
      Duration ttl = Duration.ofHours(mediaProperties.getUpload().getPendingUploadTtlHours());
      dynamoDbService.createMedia(Media.builder()
          .mediaId(mediaId)
          .size(request.getFileSize())
          .name(request.getFileName())
          .mimetype(request.getContentType())
          .status(MediaStatus.PENDING_UPLOAD)
          .width(targetWidth)
          .outputFormat(targetFormat)
          .build(), ttl);

      var headers = new LinkedHashMap<String, String>();
      headers.put("Content-Type", request.getContentType());

      span.setStatus(StatusCode.OK);
      log.info("Presigned upload initialized: mediaId={}, fileName={}, outputFormat={}", mediaId, request.getFileName(),
          targetFormat.getFormat());

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

  public Optional<Media> completePresignedUpload(String mediaId) {
    Span span = tracer.spanBuilder("complete-presigned-upload")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      span.setAttribute("media.id", mediaId);

      return dynamoDbService.getMedia(mediaId)
          .filter(media -> media.getStatus() == MediaStatus.PENDING_UPLOAD)
          .flatMap(media -> {
            // Verify file exists in S3
            if (!s3Service.objectExists(mediaId, media.getName())) {
              log.warn("File not found in S3 for mediaId: {}", mediaId);
              return Optional.empty();
            }

            // Update status to PENDING and clear TTL (record is now permanent)
            boolean updated = dynamoDbService.updateStatusConditionally(
                mediaId, MediaStatus.PENDING, MediaStatus.PENDING_UPLOAD, true);
            if (!updated) {
              log.warn("Failed to update status for mediaId: {}", mediaId);
              return Optional.empty();
            }

            // Publish event for async processing
            String outputFormat = media.getOutputFormat() != null
                ? media.getOutputFormat().getFormat()
                : OutputFormat.JPEG.getFormat();
            snsService.publishProcessMediaEvent(mediaId, media.getWidth(), outputFormat);
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
}
