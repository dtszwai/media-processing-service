package com.mediaservice.service;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.dto.MediaResponse;
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
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
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
  private final Tracer tracer;
  private final LongCounter uploadSuccessCounter;
  private final LongCounter uploadFailureCounter;

  public MediaService(DynamoDbService dynamoDbService, S3Service s3Service, SnsService snsService,
      MediaProperties mediaProperties, Tracer tracer, Meter meter) {
    this.dynamoDbService = dynamoDbService;
    this.s3Service = s3Service;
    this.snsService = snsService;
    this.mediaProperties = mediaProperties;
    this.tracer = tracer;
    this.uploadSuccessCounter = meter.counterBuilder("media.upload.success")
        .setDescription("Count of successful media uploads")
        .build();
    this.uploadFailureCounter = meter.counterBuilder("media.upload.failure")
        .setDescription("Count of failed media uploads")
        .build();
  }

  public MediaResponse uploadMedia(MultipartFile file, Integer width) throws IOException {
    String contentType = file.getContentType();
    if (contentType == null || !contentType.startsWith("image")) {
      throw new IllegalArgumentException("Invalid file type. Only images are supported.");
    }

    Span span = tracer.spanBuilder("upload-media-file")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();
    try (Scope scope = span.makeCurrent()) {
      String mediaId = UUID.randomUUID().toString();
      span.setAttribute("media.id", mediaId);

      int targetWidth = mediaProperties.resolveWidth(width);

      String fileName = file.getOriginalFilename();
      if (fileName == null || fileName.isEmpty()) {
        fileName = "image.jpg";
      }
      span.setAttribute("file.name", fileName);

      // Upload original to S3
      s3Service.uploadMedia(mediaId, fileName, file);

      // Store metadata in DynamoDB with PENDING status
      dynamoDbService.createMedia(Media.builder()
          .mediaId(mediaId)
          .size(file.getSize())
          .name(fileName)
          .mimetype(contentType)
          .status(MediaStatus.PENDING)
          .width(targetWidth)
          .build());

      // Publish event to SNS for async processing by Lambda
      snsService.publishProcessMediaEvent(mediaId, targetWidth);
      span.setStatus(StatusCode.OK);
      uploadSuccessCounter.add(1);
      log.info("Media uploaded successfully: mediaId={}, fileName={}", mediaId, fileName);
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

  public Optional<MediaStatus> getMediaStatus(String mediaId) {
    return dynamoDbService.getMedia(mediaId).map(Media::getStatus);
  }

  public Optional<Media> getMedia(String mediaId) {
    return dynamoDbService.getMedia(mediaId);
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

  public Optional<Media> resizeMedia(String mediaId, Integer width) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> {
          dynamoDbService.updateStatus(mediaId, MediaStatus.PENDING);
          snsService.publishResizeMediaEvent(mediaId, width);
          log.info("Resize request submitted for mediaId: {}", mediaId);
          return media;
        });
  }

  public Optional<Media> deleteMedia(String mediaId) {
    return dynamoDbService.getMedia(mediaId)
        .map(media -> {
          dynamoDbService.updateStatus(mediaId, MediaStatus.DELETING);
          snsService.publishDeleteMediaEvent(mediaId);
          log.info("Delete request submitted for mediaId: {}", mediaId);
          return media;
        });
  }

  public boolean mediaExists(String mediaId) {
    return dynamoDbService.getMedia(mediaId).isPresent();
  }

  public List<Media> getAllMedia() {
    return dynamoDbService.getAllMedia();
  }
}
