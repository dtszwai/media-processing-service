package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.event.MediaEvent;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.EventType;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.common.model.ProcessingJobStatus;
import com.mediaservice.lambda.config.OpenTelemetryInitializer;
import com.mediaservice.lambda.service.DynamoDbService;
import com.mediaservice.lambda.service.DocumentProcessingService;
import com.mediaservice.lambda.service.ImageProcessingService;
import com.mediaservice.lambda.service.S3Service;
import com.mediaservice.lambda.service.WebhookService;
import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.propagation.TextMapGetter;
import java.io.ByteArrayInputStream;
import java.util.HashMap;
import java.util.Map;
import javax.imageio.ImageIO;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class ManageMediaHandler implements RequestHandler<SQSEvent, String> {
  static {
    OpenTelemetryInitializer.initialize();
  }

  private static final Logger logger = LoggerFactory.getLogger(ManageMediaHandler.class);

  private static final TextMapGetter<Map<String, String>> TRACE_HEADER_GETTER = new TextMapGetter<>() {
    @Override
    public Iterable<String> keys(Map<String, String> carrier) {
      return carrier.keySet();
    }

    @Override
    public String get(Map<String, String> carrier, String key) {
      return carrier.get(key);
    }
  };

  private final DynamoDbService dynamoDbService;
  private final S3Service s3Service;
  private final ImageProcessingService imageProcessingService;
  private final DocumentProcessingService documentProcessingService;
  private final WebhookService webhookService;
  private final ObjectMapper objectMapper;
  private final OpenTelemetry openTelemetry;
  private final Tracer tracer;
  private final LongCounter deleteSuccessCounter, deleteFailureCounter;
  private final LongCounter processSuccessCounter, processFailureCounter;

  public ManageMediaHandler() {
    this(new DynamoDbService(), new S3Service(), new ImageProcessingService(), new WebhookService(), new ObjectMapper());
  }

  ManageMediaHandler(DynamoDbService dynamoDbService, S3Service s3Service,
      ImageProcessingService imageProcessingService, WebhookService webhookService, ObjectMapper objectMapper) {
    this.dynamoDbService = dynamoDbService;
    this.s3Service = s3Service;
    this.imageProcessingService = imageProcessingService;
    this.documentProcessingService = new DocumentProcessingService(objectMapper);
    this.webhookService = webhookService;
    this.objectMapper = objectMapper;

    var otel = OpenTelemetryInitializer.initialize();
    this.openTelemetry = otel;
    this.tracer = otel.getTracer("media-service-manage-media-lambda");
    var meter = otel.getMeter("media-service-manage-media-lambda");

    this.deleteSuccessCounter = counter(meter, "lambda.delete_media.success", "successful delete");
    this.deleteFailureCounter = counter(meter, "lambda.delete_media.failure", "failed delete");
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
    JsonNode bodyNode;
    try {
      bodyNode = objectMapper.readTree(message.getBody());
    } catch (Exception e) {
      throw new RuntimeException("Failed to parse message body", e);
    }

    var parentContext = extractTraceContext(bodyNode);
    var span = tracer.spanBuilder("manage-media")
        .setSpanKind(SpanKind.CONSUMER)
        .setParent(parentContext)
        .startSpan();
    try (var scope = span.makeCurrent()) {
      var event = objectMapper.readValue(bodyNode.get("Message").asText(), MediaEvent.class);
      var payload = event.getPayload();
      if (payload == null || payload.getMediaId() == null || payload.getMediaId().isEmpty()) {
        logger.warn("Skipping message with null/empty payload or mediaId");
        return;
      }

      String mediaId = payload.getMediaId();
      String tenantId = payload.getTenantId() != null ? payload.getTenantId() : "default";
      String jobId = payload.getJobId();
      String assetId = payload.getAssetId();
      String sourceAssetId = payload.getSourceAssetId();
      MediaType mediaType = MediaType.fromString(payload.getMediaType());
      if (mediaType == null) {
        mediaType = MediaType.IMAGE;
      }

      span.setAttribute("media.id", mediaId);
      span.setAttribute("tenant.id", tenantId);
      span.setAttribute("event.type", event.getType());
      span.setAttribute("media.type", mediaType.getValue());

      var eventType = EventType.fromString(event.getType());
      if (eventType == null) {
        logger.info("Skipping message with unsupported type: {}", event.getType());
        return;
      }

      switch (eventType) {
        case DELETE_MEDIA -> handleDelete(mediaId, tenantId, span);
        case PROCESS_MEDIA -> handleProcessing(mediaId, tenantId, jobId, assetId, sourceAssetId, payload.getOutput(), span);
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

  private void handleDelete(String mediaId, String tenantId, Span span) {
    logger.info("Cleaning up S3 files for soft-deleted media: {}", mediaId);
    try {
      var assets = dynamoDbService.listAssets(mediaId);
      for (MediaAsset asset : assets) {
        String extension = extensionFromFormat(asset.getOutputFormat());
        if (extension.isBlank()) {
          continue;
        }
        s3Service.deleteAsset(tenantId, mediaId, asset.getAssetId(), extension);
      }
      span.setStatus(StatusCode.OK);
      deleteSuccessCounter.add(1);
    } catch (Exception e) {
      logger.error("Failed to clean up S3 files for media {}: {}", mediaId, e.getMessage(), e);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      deleteFailureCounter.add(1);
      throw e;
    }
  }

  private void handleProcessing(String mediaId, String tenantId, String jobId, String assetId, String sourceAssetId,
      com.mediaservice.common.model.OutputSpec output, Span span) {
    if (output == null || output.getOperation() == null) {
      logger.warn("Missing output spec for job {} media {}", jobId, mediaId);
      return;
    }
    AssetOperation operation = output.getOperation();
    String outputFormat = output.getOutputFormat();
    Integer width = output.getWidth();

    try {
      if (jobId != null) {
        dynamoDbService.updateJobStatusConditionally(mediaId, jobId, ProcessingJobStatus.PROCESSING, ProcessingJobStatus.PENDING);
      }

      if (assetId == null || sourceAssetId == null) {
        throw new IllegalArgumentException("Missing assetId or sourceAssetId");
      }

      boolean statusUpdated = dynamoDbService.updateAssetStatusConditionally(mediaId, assetId, AssetStatus.PROCESSING, AssetStatus.PENDING);
      if (!statusUpdated) {
        var existing = dynamoDbService.getAsset(mediaId, assetId);
        if (existing.isPresent() && existing.get().getStatus() == AssetStatus.COMPLETE) {
          logger.info("Asset {} already COMPLETE, skipping", assetId);
          return;
        }
      }

      var sourceAsset = dynamoDbService.getAsset(mediaId, sourceAssetId)
          .orElseThrow(() -> new IllegalStateException("Source asset not found"));
      String sourceExt = extensionFromFormat(sourceAsset.getOutputFormat());
      byte[] sourceData = s3Service.downloadAsset(tenantId, mediaId, sourceAssetId, sourceExt);

      byte[] outputData;
      String contentType;
      Integer outputWidth = null;
      Integer outputHeight = null;

      switch (operation) {
        case IMAGE_PROCESS -> {
          OutputFormat format = OutputFormat.fromString(outputFormat);
          outputData = imageProcessingService.processImage(sourceData, width, format);
          contentType = format.getContentType();
        }
        case IMAGE_THUMBNAIL -> {
          OutputFormat format = OutputFormat.fromString(outputFormat);
          outputData = imageProcessingService.generateThumbnail(sourceData, format);
          contentType = format.getContentType();
        }
        case DOCUMENT_PREVIEW -> {
          var result = documentProcessingService.process(mediaId, sourceData);
          outputData = result.previewPng();
          contentType = "image/png";
          dynamoDbService.updateDocumentMetadata(mediaId, result.metadata());
        }
        case DOCUMENT_TEXT -> {
          var result = documentProcessingService.process(mediaId, sourceData);
          outputData = result.textJson();
          contentType = "application/json";
          dynamoDbService.updateDocumentMetadata(mediaId, result.metadata());
        }
        default -> throw new IllegalStateException("Unsupported operation: " + operation);
      }

      if (contentType.startsWith("image/")) {
        try (var in = new ByteArrayInputStream(outputData)) {
          var img = ImageIO.read(in);
          if (img != null) {
            outputWidth = img.getWidth();
            outputHeight = img.getHeight();
          }
        }
      }

      String extension = extensionFromFormat(outputFormat);
      boolean cachePublic = operation == AssetOperation.IMAGE_THUMBNAIL || operation == AssetOperation.DOCUMENT_PREVIEW;
      s3Service.uploadAsset(tenantId, mediaId, assetId, extension, outputData, contentType, cachePublic);
      dynamoDbService.updateAssetSuccess(mediaId, assetId, outputData.length, outputWidth, outputHeight, contentType);

      if (jobId != null) {
        dynamoDbService.updateJobStatusConditionally(mediaId, jobId, ProcessingJobStatus.COMPLETE, ProcessingJobStatus.PROCESSING);
      }

      updateMediaStatusFromAssets(mediaId);
      sendWebhookIfComplete(mediaId);

      span.setStatus(StatusCode.OK);
      processSuccessCounter.add(1);
    } catch (Exception e) {
      logger.error("Failed to process media {}: {}", mediaId, e.getMessage(), e);
      span.setStatus(StatusCode.ERROR, e.getMessage());
      try {
        dynamoDbService.updateAssetError(mediaId, assetId, e.getMessage());
        if (jobId != null) {
          dynamoDbService.updateJobError(mediaId, jobId, e.getMessage());
        }
        dynamoDbService.updateMediaStatus(mediaId, MediaStatus.ERROR);
      } catch (Exception updateErr) {
        logger.error("Failed to update status to ERROR: {}", updateErr.getMessage());
      }
      processFailureCounter.add(1);
      throw new RuntimeException("Failed to process media", e);
    }
  }

  private void updateMediaStatusFromAssets(String mediaId) {
    var mediaOpt = dynamoDbService.getMedia(mediaId);
    if (mediaOpt.isEmpty() || mediaOpt.get().getStatus() == MediaStatus.DELETED) {
      return;
    }
    var assets = dynamoDbService.listAssets(mediaId);
    boolean anyProcessing = assets.stream().anyMatch(a -> a.getStatus() == AssetStatus.PENDING || a.getStatus() == AssetStatus.PROCESSING);
    boolean anyError = assets.stream().anyMatch(a -> a.getStatus() == AssetStatus.ERROR);
    MediaStatus newStatus = resolveMediaStatus(anyProcessing, anyError);
    dynamoDbService.updateMediaStatus(mediaId, newStatus);
  }

  private MediaStatus resolveMediaStatus(boolean anyProcessing, boolean anyError) {
    if (anyProcessing) {
      return MediaStatus.PROCESSING;
    }
    if (anyError) {
      return MediaStatus.ERROR;
    }
    return MediaStatus.COMPLETE;
  }

  private void sendWebhookIfComplete(String mediaId) {
    var mediaOpt = dynamoDbService.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return;
    }
    Media media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.COMPLETE && media.getWebhookUrl() != null) {
      try {
        webhookService.sendCompletionNotification(media, media.getWebhookUrl());
      } catch (Exception webhookErr) {
        logger.warn("Webhook notification failed for media {}: {}", media.getMediaId(), webhookErr.getMessage());
      }
    }
  }

  private String extensionFromFormat(String format) {
    if (format == null || format.isBlank()) {
      return "";
    }
    return "." + format.toLowerCase();
  }

  private io.opentelemetry.context.Context extractTraceContext(JsonNode snsEnvelope) {
    var messageAttributes = snsEnvelope.get("MessageAttributes");
    if (messageAttributes == null) {
      return io.opentelemetry.context.Context.current();
    }

    Map<String, String> headers = new HashMap<>();
    var fields = messageAttributes.fields();
    while (fields.hasNext()) {
      var entry = fields.next();
      var valueNode = entry.getValue().get("Value");
      if (valueNode != null) {
        headers.put(entry.getKey(), valueNode.asText());
      }
    }

    if (headers.isEmpty()) {
      return io.opentelemetry.context.Context.current();
    }

    return openTelemetry.getPropagators().getTextMapPropagator()
        .extract(io.opentelemetry.context.Context.current(), headers, TRACE_HEADER_GETTER);
  }
}
