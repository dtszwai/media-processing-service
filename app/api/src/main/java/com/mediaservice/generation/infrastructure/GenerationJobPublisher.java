package com.mediaservice.generation.infrastructure;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.generation.GenerationStageMessage;
import com.mediaservice.common.generation.Tier;
import com.mediaservice.providers.generation.core.GenerationEventPublisher;
import com.mediaservice.providers.generation.shared.SnsOtelInjector;
import io.github.resilience4j.retry.annotation.Retry;
import io.opentelemetry.api.OpenTelemetry;
import java.util.Map;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sns.model.MessageAttributeValue;
import software.amazon.awssdk.services.sns.model.PublishRequest;

@Slf4j
@Component
@RequiredArgsConstructor
public class GenerationJobPublisher implements GenerationEventPublisher {

  private final SnsClient snsClient;
  private final ObjectMapper objectMapper;
  private final OpenTelemetry openTelemetry;

  @Value("${media.generation.topic-arn:}")
  private String topicArn;

  @Override
  @Retry(name = "snsRetry")
  public void publish(GenerationStageMessage message) {
    if (topicArn == null || topicArn.isBlank()) {
      throw new IllegalStateException("MEDIA_GENERATION_TOPIC_ARN is not configured; generation stage message not published");
    }
    String messageJson;
    try {
      messageJson = objectMapper.writeValueAsString(message);
    } catch (JsonProcessingException e) {
      throw new IllegalStateException("Failed to serialize generation stage message", e);
    }

    Map<String, MessageAttributeValue> attributes = SnsOtelInjector.injectContext(openTelemetry);
    String tier = message.getTier() != null && !message.getTier().isBlank() ? message.getTier() : Tier.FREE.wireValue();
    attributes.put("tier", MessageAttributeValue.builder().dataType("String").stringValue(tier).build());

    snsClient.publish(PublishRequest.builder()
        .topicArn(topicArn)
        .message(messageJson)
        .messageAttributes(attributes)
        .build());
  }
}
