package com.mediaservice.service;

import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.StatusResponse;
import com.mediaservice.model.Media;
import com.mediaservice.model.MediaStatus;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.util.Optional;
import java.util.UUID;

@Slf4j
@Service
public class MediaService {

  private final DynamoDbService dynamoDbService;
  private final S3Service s3Service;
  private final SnsService snsService;
  private final Tracer tracer;
  private final LongCounter uploadSuccessCounter;
  private final LongCounter uploadFailureCounter;

  @Value("${media.width.default}")
  private int defaultWidth;

  @Value("${media.width.min}")
  private int minWidth;

  @Value("${media.width.max}")
  private int maxWidth;

  public MediaService(DynamoDbService dynamoDbService, S3Service s3Service,
      SnsService snsService, Tracer tracer, Meter meter) {
    this.dynamoDbService = dynamoDbService;
    this.s3Service = s3Service;
    this.snsService = snsService;
    this.tracer = tracer;

    this.uploadSuccessCounter = meter.counterBuilder("media.upload.success")
        .setDescription("Count of successful media uploads")
        .build();

    this.uploadFailureCounter = meter.counterBuilder("media.upload.failure")
        .setDescription("Count of failed media uploads")
        .build();
  }

  public MediaResponse uploadMedia(MultipartFile file, Integer width) throws IOException {
    Span span = tracer.spanBuilder("upload-media-file")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();

    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      // Validate file type
      String contentType = file.getContentType();
      if (contentType == null || !contentType.startsWith("image")) {
        uploadFailureCounter.add(1);
        span.setStatus(StatusCode.ERROR, "Invalid file type");
        throw new IllegalArgumentException("Invalid file type. Only images are supported.");
      }

      // Use default width if not provided or out of range
      int targetWidth = (width != null && width >= minWidth && width <= maxWidth)
          ? width
          : defaultWidth;

      String fileName = file.getOriginalFilename();
      if (fileName == null || fileName.isEmpty()) {
        fileName = "image.jpg";
      }

      // Upload original to S3
      s3Service.uploadMedia(mediaId, fileName, file);

      // Store metadata in DynamoDB with PENDING status (Lambda will set to
      // PROCESSING)
      Media media = Media.builder()
          .mediaId(mediaId)
          .size(file.getSize())
          .name(fileName)
          .mimetype(contentType)
          .status(MediaStatus.PENDING)
          .width(targetWidth)
          .build();

      dynamoDbService.createMedia(media);

      // Publish event to SNS for async processing by Lambda
      snsService.publishProcessMediaEvent(mediaId, targetWidth);

      span.setAttribute("file.name", fileName);
      span.setStatus(StatusCode.OK);
      uploadSuccessCounter.add(1);

      log.info("Media uploaded successfully: mediaId={}, fileName={}", mediaId, fileName);

      return MediaResponse.builder()
          .mediaId(mediaId)
          .build();

    } catch (Exception e) {
      uploadFailureCounter.add(1);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      log.error("Failed to upload media: {}", e.getMessage());
      throw e;
    } finally {
      span.end();
    }
  }

  public Optional<StatusResponse> getMediaStatus(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> StatusResponse.builder()
            .status(media.getStatus())
            .build());
  }

  public Optional<MediaResponse> getMedia(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> MediaResponse.builder()
            .mediaId(media.getMediaId())
            .size(media.getSize())
            .name(media.getName())
            .mimetype(media.getMimetype())
            .status(media.getStatus())
            .build());
  }

  public Optional<String> getDownloadUrl(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .filter(media -> media.getStatus() == MediaStatus.COMPLETE)
        .map(media -> s3Service.getPresignedUrl(mediaId, media.getName()));
  }

  public boolean isMediaProcessing(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> media.getStatus() != MediaStatus.COMPLETE)
        .orElse(false);
  }

  public Optional<MediaResponse> resizeMedia(String mediaId, Integer width) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> {
          snsService.publishResizeMediaEvent(mediaId, width);
          log.info("Resize request submitted for mediaId: {}", mediaId);
          return MediaResponse.builder()
              .mediaId(mediaId)
              .build();
        });
  }

  public Optional<MediaResponse> deleteMedia(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> {
          snsService.publishDeleteMediaEvent(mediaId);
          log.info("Delete request submitted for mediaId: {}", mediaId);
          return MediaResponse.builder()
              .mediaId(mediaId)
              .build();
        });
  }

  public boolean mediaExists(String mediaId) {
    return dynamoDbService.getMedia(mediaId).isPresent();
  }
}
