package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.lambda.model.Media;
import com.mediaservice.lambda.model.MediaEvent;
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

import java.io.IOException;
import java.util.Optional;

/**
 * AWS Lambda handler for managing media operations.
 * Triggered by SQS events from the media management queue.
 *
 * Supported event types:
 * - media.v1.process: Process newly uploaded media (resize + watermark)
 * - media.v1.delete: Delete media from storage and database
 * - media.v1.resize: Resize existing media to a new width
 */
public class ManageMediaHandler implements RequestHandler<SQSEvent, String> {

  private static final Logger logger = LoggerFactory.getLogger(ManageMediaHandler.class);
  private static final String DELETE_EVENT_TYPE = "media.v1.delete";
  private static final String RESIZE_EVENT_TYPE = "media.v1.resize";
  private static final String PROCESS_EVENT_TYPE = "media.v1.process";

  private final DynamoDbService dynamoDbService;
  private final S3Service s3Service;
  private final ImageProcessingService imageProcessingService;
  private final ObjectMapper objectMapper;
  private final Tracer tracer;
  private final LongCounter deleteSuccessCounter;
  private final LongCounter deleteFailureCounter;
  private final LongCounter resizeSuccessCounter;
  private final LongCounter resizeFailureCounter;
  private final LongCounter processSuccessCounter;
  private final LongCounter processFailureCounter;

  public ManageMediaHandler() {
    this.dynamoDbService = new DynamoDbService();
    this.s3Service = new S3Service();
    this.imageProcessingService = new ImageProcessingService();
    this.objectMapper = new ObjectMapper();

    // Initialize OpenTelemetry
    this.tracer = GlobalOpenTelemetry.getTracer("media-service-manage-media-lambda");
    Meter meter = GlobalOpenTelemetry.getMeter("media-service-manage-media-lambda");

    this.deleteSuccessCounter = meter.counterBuilder("lambda.delete_media.success")
        .setDescription("Count of successful media delete operations")
        .build();

    this.deleteFailureCounter = meter.counterBuilder("lambda.delete_media.failure")
        .setDescription("Count of failed media delete operations")
        .build();

    this.resizeSuccessCounter = meter.counterBuilder("lambda.resize_media.success")
        .setDescription("Count of successful media resize operations")
        .build();

    this.resizeFailureCounter = meter.counterBuilder("lambda.resize_media.failure")
        .setDescription("Count of failed media resize operations")
        .build();

    this.processSuccessCounter = meter.counterBuilder("lambda.process_media.success")
        .setDescription("Count of successful media processing operations")
        .build();

    this.processFailureCounter = meter.counterBuilder("lambda.process_media.failure")
        .setDescription("Count of failed media processing operations")
        .build();
  }

  @Override
  public String handleRequest(SQSEvent sqsEvent, Context context) {
    logger.info("ManageMedia Lambda invoked");

    for (SQSEvent.SQSMessage message : sqsEvent.getRecords()) {
      processMessage(message);
    }

    return "OK";
  }

  private void processMessage(SQSEvent.SQSMessage message) {
    Span span = tracer.spanBuilder("manage-media")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();

    try (Scope scope = span.makeCurrent()) {
      // Parse the SNS wrapper message
      JsonNode bodyNode = objectMapper.readTree(message.getBody());
      String snsMessage = bodyNode.get("Message").asText();

      // Parse the actual event
      MediaEvent event = objectMapper.readValue(snsMessage, MediaEvent.class);
      String eventType = event.getType();
      MediaEvent.MediaEventPayload payload = event.getPayload();

      if (payload == null) {
        logger.warn("Event payload is null, skipping message");
        return;
      }

      String mediaId = payload.getMediaId();
      Integer width = payload.getWidth();

      span.setAttribute("media.id", mediaId);
      span.setAttribute("event.type", eventType);

      if (width != null) {
        span.setAttribute("width", width);
      }

      logger.info("Processing event: type={}, mediaId={}", eventType, mediaId);

      switch (eventType) {
        case DELETE_EVENT_TYPE:
          handleDelete(mediaId, span);
          break;
        case RESIZE_EVENT_TYPE:
          if (width == null) {
            logger.info("Skipping resize message with missing width");
            break;
          }
          handleMediaProcessing(mediaId, width, MediaStatus.COMPLETE,
              imageProcessingService::resizeImage, span, resizeSuccessCounter, resizeFailureCounter);
          break;
        case PROCESS_EVENT_TYPE:
          handleMediaProcessing(mediaId, width, MediaStatus.PENDING,
              imageProcessingService::processImage, span, processSuccessCounter, processFailureCounter);
          break;
        default:
          logger.info("Skipping message with unsupported type: {}", eventType);
      }

    } catch (Exception e) {
      logger.error("Failed to process message: {}", e.getMessage(), e);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw new RuntimeException("Failed to process message", e);
    } finally {
      span.end();
    }
  }

  private void handleDelete(String mediaId, Span span) {
    if (mediaId == null || mediaId.isEmpty()) {
      logger.info("Skipping delete message with no mediaId");
      return;
    }

    logger.info("Deleting media: {}", mediaId);

    try {
      Optional<Media> mediaOpt = dynamoDbService.deleteMedia(mediaId);

      if (mediaOpt.isEmpty()) {
        logger.info("Media {} not found, nothing to delete", mediaId);
        return;
      }

      Media media = mediaOpt.get();
      String mediaName = media.getName();
      MediaStatus status = media.getStatus();

      // Delete uploaded file
      s3Service.deleteMediaFile(mediaId, mediaName, "uploads");

      // Delete resized file if processing was complete
      if (status != MediaStatus.PROCESSING) {
        String keyPrefix = (status == MediaStatus.ERROR) ? "uploads" : "resized";
        s3Service.deleteMediaFile(mediaId, mediaName, keyPrefix);
      }

      logger.info("Deleted media: {}", mediaId);
      span.setStatus(StatusCode.OK);
      deleteSuccessCounter.add(1);

    } catch (Exception e) {
      logger.error("Failed to delete media {}: {}", mediaId, e.getMessage(), e);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      deleteFailureCounter.add(1);
      throw e;
    }
  }

  private void handleMediaProcessing(String mediaId, Integer requestedWidth, MediaStatus expectedStatus,
                                     ImageProcessor processor, Span span,
                                     LongCounter successCounter, LongCounter failureCounter) {
    if (mediaId == null || mediaId.isEmpty()) {
      logger.info("Skipping message with no mediaId");
      return;
    }

    logger.info("Processing/Resizing media: {}", mediaId);

    try {
      // Set status to PROCESSING
      Optional<Media> mediaOpt = dynamoDbService.setMediaStatusConditionally(
          mediaId,
          MediaStatus.PROCESSING,
          expectedStatus);

      if (mediaOpt.isEmpty()) {
        logger.warn("Media {} not found or not in {} status", mediaId, expectedStatus);
        return;
      }

      Media media = mediaOpt.get();
      String mediaName = media.getName();

      logger.info("Media status set to PROCESSING");

      // Get the file from S3
      byte[] imageData = s3Service.getMediaFile(mediaId, mediaName);
      logger.info("Retrieved media file from S3");

      // Determine target width
      Integer targetWidth = (requestedWidth != null) ? requestedWidth : media.getWidth();

      // Process the image
      long processingStart = System.currentTimeMillis();
      byte[] processedImage = processor.process(imageData, targetWidth);
      long processingDuration = System.currentTimeMillis() - processingStart;

      span.addEvent("image.processing.done", Attributes.of(
          AttributeKey.longKey("media.processing.duration"), processingDuration));
      logger.info("Processed/Resized media in {} ms", processingDuration);

      // Upload processed image to S3
      s3Service.uploadMedia(mediaId, mediaName, processedImage, "resized");
      logger.info("Uploaded processed media to S3");

      // Set status to COMPLETE
      dynamoDbService.setMediaStatusConditionally(
          mediaId,
          MediaStatus.COMPLETE,
          MediaStatus.PROCESSING);

      logger.info("Media operation complete for: {}", mediaId);
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

      // Set status to ERROR
      try {
        dynamoDbService.setMediaStatus(mediaId, MediaStatus.ERROR);
      } catch (Exception updateError) {
        logger.error("Failed to update status to ERROR: {}", updateError.getMessage());
      }

      failureCounter.add(1);
      throw new RuntimeException("Failed to process media", e);
    }
  }

  @FunctionalInterface
  private interface ImageProcessor {
    byte[] process(byte[] data, Integer width) throws IOException;
  }
}
