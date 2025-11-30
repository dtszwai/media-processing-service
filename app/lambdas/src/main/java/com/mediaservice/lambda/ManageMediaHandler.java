package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.lambda.config.OpenTelemetryInitializer;
import com.mediaservice.lambda.model.MediaEvent;
import com.mediaservice.lambda.model.MediaStatus;
import com.mediaservice.lambda.model.OutputFormat;
import com.mediaservice.lambda.service.DynamoDbService;
import com.mediaservice.lambda.service.ImageProcessingService;
import com.mediaservice.lambda.service.S3Service;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;

import java.io.IOException;

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
  static {
    OpenTelemetryInitializer.initialize();
  }

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

    // Initialize OpenTelemetry with OTLP exporters
    var openTelemetry = OpenTelemetryInitializer.initialize();
    this.tracer = openTelemetry.getTracer("media-service-manage-media-lambda");
    var meter = openTelemetry.getMeter("media-service-manage-media-lambda");

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
    try {
      for (var message : sqsEvent.getRecords()) {
        processMessage(message);
      }
      return "OK";
    } finally {
      OpenTelemetryInitializer.flush();
    }
  }

  private void processMessage(SQSEvent.SQSMessage message) {
    var span = tracer.spanBuilder("manage-media").setSpanKind(SpanKind.INTERNAL).startSpan();
    try (var scope = span.makeCurrent()) {
      // Parse the SNS wrapper message
      var bodyNode = objectMapper.readTree(message.getBody());
      var snsMessage = bodyNode.get("Message").asText();

      // Parse the actual event
      var event = objectMapper.readValue(snsMessage, MediaEvent.class);
      var eventType = event.getType();
      var payload = event.getPayload();
      if (payload == null) {
        logger.warn("Event payload is null, skipping message");
        return;
      }
      var mediaId = payload.getMediaId();
      if (mediaId == null || mediaId.isEmpty()) {
        logger.info("Skipping message with no mediaId");
        return;
      }
      var width = payload.getWidth();
      var outputFormatStr = payload.getOutputFormat();
      var outputFormat = OutputFormat.fromString(outputFormatStr);
      span.setAttribute("media.id", mediaId);
      span.setAttribute("event.type", eventType);
      if (width != null) {
        span.setAttribute("width", width);
      }
      span.setAttribute("output.format", outputFormat.getFormat());
      logger.info("Processing event: type={}, mediaId={}, outputFormat={}", eventType, mediaId, outputFormat.getFormat());
      switch (eventType) {
        case DELETE_EVENT_TYPE:
          handleDelete(mediaId, span);
          break;
        case RESIZE_EVENT_TYPE:
          if (width == null) {
            logger.info("Skipping resize message with missing width");
            break;
          }
          handleMediaProcessing(mediaId, width, outputFormat, MediaStatus.PENDING, true, span);
          break;
        case PROCESS_EVENT_TYPE:
          handleMediaProcessing(mediaId, width, outputFormat, MediaStatus.PENDING, false, span);
          break;
        default:
          logger.info("Skipping message with unsupported type: {}", eventType);
      }
    } catch (Exception e) {
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      throw new RuntimeException("Failed to process message", e);
    } finally {
      span.end();
    }
  }

  private void handleDelete(String mediaId, Span span) {
    logger.info("Deleting media: {}", mediaId);
    try {
      var mediaOpt = dynamoDbService.deleteMedia(mediaId);
      if (mediaOpt.isEmpty()) {
        logger.info("Media {} not found, nothing to delete", mediaId);
        return;
      }
      var media = mediaOpt.get();
      var mediaName = media.getName();
      var status = media.getStatus();
      var outputFormat = media.getOutputFormat() != null ? media.getOutputFormat() : OutputFormat.JPEG;
      // Delete uploaded file
      s3Service.deleteMediaFile(mediaId, mediaName, "uploads");
      // Delete resized file if processing was complete
      if (status != MediaStatus.PROCESSING) {
        if (status == MediaStatus.ERROR) {
          s3Service.deleteMediaFile(mediaId, mediaName, "uploads");
        } else {
          s3Service.deleteMediaFileWithFormat(mediaId, mediaName, "resized", outputFormat);
        }
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

  private void handleMediaProcessing(String mediaId, Integer requestedWidth, OutputFormat outputFormat,
      MediaStatus expectedStatus, boolean isResize, Span span) {
    var successCounter = isResize ? resizeSuccessCounter : processSuccessCounter;
    var failureCounter = isResize ? resizeFailureCounter : processFailureCounter;
    logger.info("Processing/Resizing media: {} with outputFormat: {}", mediaId, outputFormat.getFormat());
    try {
      // Set status to PROCESSING
      var mediaOpt = dynamoDbService.setMediaStatusConditionally(mediaId, MediaStatus.PROCESSING, expectedStatus);
      if (mediaOpt.isEmpty()) {
        logger.warn("Media {} not found or not in {} status", mediaId, expectedStatus);
        return;
      }
      var media = mediaOpt.get();
      var mediaName = media.getName();
      logger.info("Media status set to PROCESSING");
      // Get the file from S3
      byte[] imageData = s3Service.getMediaFile(mediaId, mediaName);
      logger.info("Retrieved media file from S3");
      // Determine target width and output format
      var targetWidth = (requestedWidth != null) ? requestedWidth : media.getWidth();
      var targetFormat = outputFormat != null ? outputFormat
          : (media.getOutputFormat() != null ? media.getOutputFormat() : OutputFormat.JPEG);
      // Process the image
      long processingStart = System.currentTimeMillis();
      byte[] processedImage = isResize
          ? imageProcessingService.resizeImage(imageData, targetWidth, targetFormat)
          : imageProcessingService.processImage(imageData, targetWidth, targetFormat);
      long processingDuration = System.currentTimeMillis() - processingStart;
      span.addEvent("image.processing.done",
          Attributes.of(AttributeKey.longKey("media.processing.duration"), processingDuration));
      logger.info("Processed/Resized media in {} ms with format: {}", processingDuration, targetFormat.getFormat());
      // Upload processed image to S3
      s3Service.uploadMedia(mediaId, mediaName, processedImage, "resized", targetFormat);
      logger.info("Uploaded processed media to S3");
      // Set status to COMPLETE and update width
      dynamoDbService.setMediaStatusConditionally(mediaId, MediaStatus.COMPLETE, MediaStatus.PROCESSING,
          targetWidth);
      logger.info("Media operation complete for: {}", mediaId);
      span.setStatus(StatusCode.OK);
      successCounter.add(1);
    } catch (ConditionalCheckFailedException e) {
      var actualStatus = dynamoDbService.getMedia(mediaId).map(m -> m.getStatus().name()).orElse("NOT_FOUND");
      logger.error("Conditional check failed for media {}: expected={}, actual={}", mediaId, expectedStatus, actualStatus);
      span.setStatus(StatusCode.ERROR, "expected=" + expectedStatus + ", actual=" + actualStatus);
      failureCounter.add(1);
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
}
