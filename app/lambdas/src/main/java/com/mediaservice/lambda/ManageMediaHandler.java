package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.lambda.config.OpenTelemetryInitializer;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.event.MediaEvent;
import com.mediaservice.common.model.EventType;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.lambda.service.DynamoDbService;
import com.mediaservice.lambda.service.ImageProcessingService;
import com.mediaservice.lambda.service.S3Service;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;

public class ManageMediaHandler implements RequestHandler<SQSEvent, String> {
  static {
    OpenTelemetryInitializer.initialize();
  }

  private static final Logger logger = LoggerFactory.getLogger(ManageMediaHandler.class);

  private final DynamoDbService dynamoDbService;
  private final S3Service s3Service;
  private final ImageProcessingService imageProcessingService;
  private final ObjectMapper objectMapper;
  private final Tracer tracer;
  private final LongCounter deleteSuccessCounter, deleteFailureCounter;
  private final LongCounter resizeSuccessCounter, resizeFailureCounter;
  private final LongCounter processSuccessCounter, processFailureCounter;

  /**
   * Default constructor for AWS Lambda runtime.
   * Creates all dependencies with default implementations.
   */
  public ManageMediaHandler() {
    this(new DynamoDbService(), new S3Service(), new ImageProcessingService(), new ObjectMapper());
  }

  /**
   * Constructor for testing - allows injection of mock/stub dependencies.
   */
  ManageMediaHandler(DynamoDbService dynamoDbService, S3Service s3Service,
      ImageProcessingService imageProcessingService, ObjectMapper objectMapper) {
    this.dynamoDbService = dynamoDbService;
    this.s3Service = s3Service;
    this.imageProcessingService = imageProcessingService;
    this.objectMapper = objectMapper;

    var otel = OpenTelemetryInitializer.initialize();
    this.tracer = otel.getTracer("media-service-manage-media-lambda");
    var meter = otel.getMeter("media-service-manage-media-lambda");

    this.deleteSuccessCounter = counter(meter, "lambda.delete_media.success", "successful delete");
    this.deleteFailureCounter = counter(meter, "lambda.delete_media.failure", "failed delete");
    this.resizeSuccessCounter = counter(meter, "lambda.resize_media.success", "successful resize");
    this.resizeFailureCounter = counter(meter, "lambda.resize_media.failure", "failed resize");
    this.processSuccessCounter = counter(meter, "lambda.process_media.success", "successful process");
    this.processFailureCounter = counter(meter, "lambda.process_media.failure", "failed process");
  }

  private static LongCounter counter(Meter meter, String name, String desc) {
    return meter.counterBuilder(name).setDescription("Count of " + desc + " operations").build();
  }

  @Override
  public String handleRequest(SQSEvent sqsEvent, Context context) {
    logger.info("ManageMedia Lambda invoked");
    try {
      sqsEvent.getRecords().forEach(this::processMessage);
      return "OK";
    } finally {
      OpenTelemetryInitializer.flush();
    }
  }

  private void processMessage(SQSEvent.SQSMessage message) {
    var span = tracer.spanBuilder("manage-media").setSpanKind(SpanKind.INTERNAL).startSpan();
    try (var scope = span.makeCurrent()) {
      var bodyNode = objectMapper.readTree(message.getBody());
      var event = objectMapper.readValue(bodyNode.get("Message").asText(), MediaEvent.class);

      var payload = event.getPayload();
      if (payload == null || payload.getMediaId() == null || payload.getMediaId().isEmpty()) {
        logger.warn("Skipping message with null/empty payload or mediaId");
        return;
      }

      var mediaId = payload.getMediaId();
      var width = payload.getWidth();
      var outputFormat = OutputFormat.fromString(payload.getOutputFormat());

      span.setAttribute("media.id", mediaId);
      span.setAttribute("event.type", event.getType());
      span.setAttribute("output.format", outputFormat.getFormat());
      if (width != null)
        span.setAttribute("width", width);
      logger.info("Processing event: type={}, mediaId={}, outputFormat={}", event.getType(), mediaId,
          outputFormat.getFormat());
      var eventType = EventType.fromString(event.getType());
      if (eventType == null) {
        logger.info("Skipping message with unsupported type: {}", event.getType());
        return;
      }
      switch (eventType) {
        case DELETE_MEDIA -> handleDelete(mediaId, span);
        case RESIZE_MEDIA -> {
          if (width == null) {
            logger.info("Skipping resize message with missing width");
          } else {
            handleMediaProcessing(mediaId, width, outputFormat, true, span);
          }
        }
        case PROCESS_MEDIA -> handleMediaProcessing(mediaId, width, outputFormat, false, span);
        default -> logger.info("Skipping message with unhandled event type: {}", event.getType());
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
      var outputFormat = media.getOutputFormat() != null ? media.getOutputFormat() : OutputFormat.JPEG;

      s3Service.deleteMediaFile(mediaId, media.getName(), StorageConstants.S3_PREFIX_UPLOADS);
      if (media.getStatus() != MediaStatus.PROCESSING) {
        if (media.getStatus() == MediaStatus.ERROR) {
          s3Service.deleteMediaFile(mediaId, media.getName(), StorageConstants.S3_PREFIX_UPLOADS);
        } else {
          s3Service.deleteMediaFileWithFormat(mediaId, media.getName(), StorageConstants.S3_PREFIX_RESIZED,
              outputFormat);
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
      boolean isResize, Span span) {
    var successCounter = isResize ? resizeSuccessCounter : processSuccessCounter;
    var failureCounter = isResize ? resizeFailureCounter : processFailureCounter;

    logger.info("Processing media: {} with outputFormat: {}", mediaId, outputFormat.getFormat());
    try {
      var mediaOpt = dynamoDbService.setMediaStatusConditionally(mediaId, MediaStatus.PROCESSING, MediaStatus.PENDING);
      if (mediaOpt.isEmpty()) {
        logger.warn("Media {} not found or not in PENDING status", mediaId);
        return;
      }

      var media = mediaOpt.get();
      byte[] imageData = s3Service.getMediaFile(mediaId, media.getName());

      var targetWidth = requestedWidth != null ? requestedWidth : media.getWidth();
      var targetFormat = outputFormat != null ? outputFormat
          : (media.getOutputFormat() != null ? media.getOutputFormat() : OutputFormat.JPEG);

      long start = System.currentTimeMillis();
      byte[] processed = isResize
          ? imageProcessingService.resizeImage(imageData, targetWidth, targetFormat)
          : imageProcessingService.processImage(imageData, targetWidth, targetFormat);
      long duration = System.currentTimeMillis() - start;

      span.addEvent("image.processing.done",
          Attributes.of(AttributeKey.longKey("media.processing.duration"), duration));
      logger.info("Processed media in {} ms with format: {}", duration, targetFormat.getFormat());

      s3Service.uploadMedia(mediaId, media.getName(), processed, StorageConstants.S3_PREFIX_RESIZED, targetFormat);
      dynamoDbService.setMediaStatusConditionally(mediaId, MediaStatus.COMPLETE, MediaStatus.PROCESSING, targetWidth);

      logger.info("Media operation complete for: {}", mediaId);
      span.setStatus(StatusCode.OK);
      successCounter.add(1);
    } catch (ConditionalCheckFailedException e) {
      var actual = dynamoDbService.getMedia(mediaId).map(m -> m.getStatus().name()).orElse("NOT_FOUND");
      logger.error("Conditional check failed for media {}: actual={}", mediaId, actual);
      span.setStatus(StatusCode.ERROR, "actual=" + actual);
      failureCounter.add(1);
      throw e;
    } catch (Exception e) {
      logger.error("Failed to process media {}: {}", mediaId, e.getMessage(), e);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      try {
        dynamoDbService.setMediaStatus(mediaId, MediaStatus.ERROR);
      } catch (Exception updateErr) {
        logger.error("Failed to update status to ERROR: {}", updateErr.getMessage());
      }
      failureCounter.add(1);
      throw new RuntimeException("Failed to process media", e);
    }
  }
}
