package com.mediaservice.media.infrastructure.messaging;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.event.MediaEvent;
import com.mediaservice.common.model.EventType;
import com.mediaservice.common.model.ProcessingJob;
import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.context.Context;
import io.opentelemetry.context.propagation.TextMapSetter;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sns.model.MessageAttributeValue;
import software.amazon.awssdk.services.sns.model.PublishRequest;

import java.util.HashMap;
import java.util.Map;

@Slf4j
@Service
@RequiredArgsConstructor
public class MediaEventPublisher {

  private static final TextMapSetter<Map<String, MessageAttributeValue>> SNS_SETTER =
      (carrier, key, value) -> carrier.put(key, MessageAttributeValue.builder()
          .dataType("String")
          .stringValue(value)
          .build());

  private final SnsClient snsClient;
  private final ObjectMapper objectMapper;
  private final OpenTelemetry openTelemetry;

  @Value("${aws.sns.topic-arn}")
  private String topicArn;

  public void publishProcessingJob(ProcessingJob job, String mediaType) {
    publishEvent(MediaEvent.forJob(EventType.PROCESS_MEDIA, job, mediaType));
    log.info("Published processing job event for mediaId: {} assetId: {} operation: {}",
        job.getMediaId(), job.getAssetId(), job.getOperation());
  }

  public void publishDeleteMediaEvent(String mediaId, String tenantId, String mediaType) {
    publishEvent(MediaEvent.forDelete(EventType.DELETE_MEDIA, mediaId, tenantId, mediaType));
    log.info("Published delete media event for mediaId: {} (mediaType: {})", mediaId, mediaType);
  }

  private void publishEvent(MediaEvent event) {
    try {
      String messageJson = objectMapper.writeValueAsString(event);

      Map<String, MessageAttributeValue> messageAttributes = new HashMap<>();
      openTelemetry.getPropagators().getTextMapPropagator()
          .inject(Context.current(), messageAttributes, SNS_SETTER);

      PublishRequest request = PublishRequest.builder()
          .topicArn(topicArn)
          .message(messageJson)
          .messageAttributes(messageAttributes)
          .build();
      snsClient.publish(request);
    } catch (JsonProcessingException e) {
      throw new IllegalStateException("Failed to serialize event message", e);
    }
  }
}
