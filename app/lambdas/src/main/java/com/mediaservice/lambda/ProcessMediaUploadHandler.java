package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.amazonaws.services.lambda.runtime.events.S3Event;
import com.amazonaws.services.lambda.runtime.events.models.s3.S3EventNotification;
import com.mediaservice.lambda.model.Media;
import com.mediaservice.lambda.model.MediaStatus;
import com.mediaservice.lambda.service.DynamoDbService;
import com.mediaservice.lambda.service.ImageProcessingService;
import com.mediaservice.lambda.service.S3Service;
import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;

import java.util.Optional;

/**
 * AWS Lambda handler for processing uploaded media files.
 * Triggered by S3 events when a new file is uploaded.
 *
 * This handler:
 * 1. Retrieves the uploaded image from S3
 * 2. Resizes the image to the configured width
 * 3. Adds a watermark
 * 4. Uploads the processed image to the 'resized' prefix
 * 5. Updates the media status in DynamoDB
 */
public class ProcessMediaUploadHandler implements RequestHandler<S3Event, String> {

  private static final Logger logger = LoggerFactory.getLogger(ProcessMediaUploadHandler.class);

  private final DynamoDbService dynamoDbService;
  private final S3Service s3Service;
  private final ImageProcessingService imageProcessingService;
  private final Tracer tracer;
  private final LongCounter successCounter;
  private final LongCounter failureCounter;

  public ProcessMediaUploadHandler() {
    this.dynamoDbService = new DynamoDbService();
    this.s3Service = new S3Service();
    this.imageProcessingService = new ImageProcessingService();

    // Initialize OpenTelemetry
    this.tracer = GlobalOpenTelemetry.getTracer("media-service-process-media-upload-lambda");
    Meter meter = GlobalOpenTelemetry.getMeter("media-service-process-media-upload-lambda");

    this.successCounter = meter.counterBuilder("lambda.process_media.success")
        .setDescription("Count of successful media processing operations")
        .build();

    this.failureCounter = meter.counterBuilder("lambda.process_media.failure")
        .setDescription("Count of failed media processing operations")
        .build();
  }

  @Override
  public String handleRequest(S3Event s3Event, Context context) {
    logger.info("ProcessMediaUpload Lambda invoked");

    for (S3EventNotification.S3EventNotificationRecord record : s3Event.getRecords()) {
      String s3Key = record.getS3().getObject().getKey();
      String mediaId = extractMediaId(s3Key);

      if (mediaId == null) {
        logger.warn("Could not extract mediaId from S3 key: {}", s3Key);
        continue;
      }

      processMedia(mediaId);
    }

    return "OK";
  }

  private void processMedia(String mediaId) {
    Span span = tracer.spanBuilder("process-media-upload")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();

    try (Scope scope = span.makeCurrent()) {
      span.setAttribute("media.id", mediaId);
      logger.info("Processing media: {}", mediaId);

      // Set status to PROCESSING
      Optional<Media> mediaOpt = dynamoDbService.setMediaStatusConditionally(
          mediaId,
          MediaStatus.PROCESSING,
          MediaStatus.PENDING);

      if (mediaOpt.isEmpty()) {
        logger.warn("Media {} not found or already processing", mediaId);
        return;
      }

      Media media = mediaOpt.get();
      String mediaName = media.getName();
      Integer width = media.getWidth();

      logger.info("Media status set to PROCESSING");

      // Get the uploaded file from S3
      byte[] imageData = s3Service.getMediaFile(mediaId, mediaName);
      logger.info("Retrieved media file from S3");

      // Process the image
      long processingStart = System.currentTimeMillis();
      byte[] processedImage = imageProcessingService.processImage(imageData, width);
      long processingDuration = System.currentTimeMillis() - processingStart;

      span.addEvent("sharp.resizing.done", Attributes.of(
          AttributeKey.longKey("media.processing.duration"), processingDuration));
      logger.info("Processed media in {} ms", processingDuration);

      // Upload processed image to S3
      s3Service.uploadMedia(mediaId, mediaName, processedImage, "resized");
      logger.info("Uploaded processed media to S3");

      // Set status to COMPLETE
      dynamoDbService.setMediaStatusConditionally(
          mediaId,
          MediaStatus.COMPLETE,
          MediaStatus.PROCESSING);
      logger.info("Media processing complete for: {}", mediaId);

      span.setStatus(StatusCode.OK);
      successCounter.add(1);

    } catch (ConditionalCheckFailedException e) {
      logger.error("Conditional check failed for media {}: {}", mediaId, e.getMessage());
      span.setStatus(StatusCode.ERROR, "Conditional check failed");
      failureCounter.add(1, Attributes.of(
          AttributeKey.stringKey("reason"), "CONDITIONAL_CHECK_FAILURE"));
      throw e;
    } catch (Exception e) {
      logger.error("Failed to process media {}: {}", mediaId, e.getMessage(), e);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);

      // Set status to ERROR
      try {
        dynamoDbService.setMediaStatus(mediaId, MediaStatus.ERROR);
      } catch (Exception updateError) {
        logger.error("Failed to update status to ERROR: {}", updateError.getMessage());
      }

      failureCounter.add(1);
      throw new RuntimeException("Failed to process media", e);
    } finally {
      span.end();
    }
  }

  /**
   * Extracts the mediaId from an S3 key.
   * Expected format: uploads/{mediaId}/{filename}
   */
  private String extractMediaId(String s3Key) {
    if (s3Key == null || !s3Key.startsWith("uploads/")) {
      return null;
    }

    String[] parts = s3Key.split("/");
    if (parts.length >= 2) {
      return parts[1];
    }
    return null;
  }
}
