package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.mediaservice.common.generation.GenerationStageMessage;
import com.mediaservice.common.generation.Tier;
import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.lambda.config.OpenTelemetryInitializer;
import com.mediaservice.lambda.service.S3Service;
import com.mediaservice.lambda.service.WebhookService;
import com.mediaservice.providers.generation.shared.BoundedRetry;
import com.mediaservice.providers.generation.core.DynamoDbGenerationRepository;
import com.mediaservice.providers.generation.core.GeneratedAssetStorage;
import com.mediaservice.providers.generation.core.GenerationAdmissionController;
import com.mediaservice.providers.generation.core.GenerationEventPublisher;
import com.mediaservice.providers.generation.core.GenerationProviderFactory;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;
import com.mediaservice.providers.generation.core.GenerationWorkflow;
import com.mediaservice.providers.generation.core.OpenTelemetryGenerationMetrics;
import com.mediaservice.providers.generation.core.WebhookNotifier;
import com.mediaservice.providers.generation.shared.SnsOtelInjector;
import io.opentelemetry.api.OpenTelemetry;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.services.sns.model.MessageAttributeValue;
import software.amazon.awssdk.services.sns.model.PublishRequest;

public class GenerationWorkerHandler implements RequestHandler<SQSEvent, Void> {
  private static final Logger logger = LoggerFactory.getLogger(GenerationWorkerHandler.class);
  private final ObjectMapper objectMapper;
  private final GenerationWorkflow workflow;

  public GenerationWorkerHandler() {
    this.objectMapper = new ObjectMapper().registerModule(new JavaTimeModule());
    LambdaConfig config = LambdaConfig.getInstance();
    var repository = new DynamoDbGenerationRepository(AwsClientFactory.getDynamoDbClient(), config.getTableName());
    var runtimeConfig = GenerationRuntimeConfig.fromEnvironment(System.getenv());
    var providerFactory = new GenerationProviderFactory(
        runtimeConfig,
        AwsClientFactory.getDynamoDbClient(),
        config.getTableName());
    var openTelemetry = OpenTelemetryInitializer.initialize();
    var meter = openTelemetry.getMeter("media-service-generation-lambda");
    this.workflow = new GenerationWorkflow(
        repository,
        providerFactory,
        new LambdaGeneratedAssetStorage(new S3Service()),
        new LambdaGenerationPublisher(objectMapper, config.getGenerationTopicArn(), openTelemetry),
        new LambdaWebhookNotifier(new WebhookService()),
        GenerationAdmissionController.allowAll(),
        new OpenTelemetryGenerationMetrics(meter, "generation-worker", runtimeConfig.region()));
  }

  GenerationWorkerHandler(GenerationWorkflow workflow, ObjectMapper objectMapper) {
    this.workflow = workflow;
    this.objectMapper = objectMapper;
  }

  @Override
  public Void handleRequest(SQSEvent event, Context context) {
    try {
      for (SQSEvent.SQSMessage record : event.getRecords()) {
        try {
          GenerationStageMessage message = parseRawMessage(record.getBody());
          logger.info("Processing generation stage: jobId={}, stage={}, attempt={}",
              message.getJobId(), message.getStage(), message.getAttempt());
          workflow.processStage(message);
        } catch (Exception e) {
          logger.error("Generation worker failed for SQS message {}: {}", record.getMessageId(), e.getMessage(), e);
          throw new RuntimeException(e);
        }
      }
      return null;
    } finally {
      OpenTelemetryInitializer.flush();
    }
  }

  private GenerationStageMessage parseRawMessage(String body) throws Exception {
    return GenerationStageMessage.fromSqsBody(body, objectMapper);
  }

  private static class LambdaGeneratedAssetStorage implements GeneratedAssetStorage {
    private final S3Service s3Service;

    private LambdaGeneratedAssetStorage(S3Service s3Service) {
      this.s3Service = s3Service;
    }

    @Override
    public void put(String tenantId, String mediaId, String assetId, com.mediaservice.common.generation.provider.Artifact artifact) {
      s3Service.uploadAsset(tenantId, mediaId, assetId, artifact.extension(), artifact.bytes(), artifact.contentType(), true);
    }

    @Override
    public String presignedUrl(String tenantId, String mediaId, String assetId, String extension, String downloadName,
        String contentType) {
      throw new UnsupportedOperationException("Generation worker does not issue presigned URLs");
    }
  }

  private static class LambdaGenerationPublisher implements GenerationEventPublisher {
    private final ObjectMapper objectMapper;
    private final String topicArn;
    private final OpenTelemetry openTelemetry;

    private LambdaGenerationPublisher(ObjectMapper objectMapper, String topicArn, OpenTelemetry openTelemetry) {
      this.objectMapper = objectMapper;
      this.topicArn = topicArn;
      this.openTelemetry = openTelemetry;
    }

    @Override
    public void publish(GenerationStageMessage message) {
      if (topicArn == null || topicArn.isBlank()) {
        throw new IllegalStateException("MEDIA_GENERATION_TOPIC_ARN is not configured for generation-worker");
      }
      String tier = message.getTier() != null && !message.getTier().isBlank() ? message.getTier() : Tier.FREE.wireValue();
      Map<String, MessageAttributeValue> attributes = SnsOtelInjector.injectContext(openTelemetry);
      attributes.put("tier", MessageAttributeValue.builder().dataType("String").stringValue(tier).build());
      try {
        AwsClientFactory.getSnsClient().publish(PublishRequest.builder()
            .topicArn(topicArn)
            .message(objectMapper.writeValueAsString(message))
            .messageAttributes(attributes)
            .build());
      } catch (Exception e) {
        throw new IllegalStateException("Failed to publish next generation stage", e);
      }
    }
  }

  private static class LambdaWebhookNotifier implements WebhookNotifier {
    private final WebhookService webhookService;

    private LambdaWebhookNotifier(WebhookService webhookService) {
      this.webhookService = webhookService;
    }

    @Override
    public void notifyComplete(com.mediaservice.common.model.Media media) {
      if (media == null || media.getWebhookUrl() == null || media.getWebhookUrl().isBlank()) {
        return;
      }
      try {
        BoundedRetry.run(3, 500L, 8_000L,
            () -> webhookService.sendCompletionNotification(media, media.getWebhookUrl()));
      } catch (Exception e) {
        // Drop after retries; job is durable in DynamoDB and clients can poll the result endpoint.
        logger.warn("Generation webhook failed after retries for media {}: {}", media.getMediaId(), e.getMessage());
      }
    }
  }
}
