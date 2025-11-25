package com.mediaservice.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.model.EventType;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sns.model.PublishRequest;
import software.amazon.awssdk.services.sns.model.SnsException;

import java.util.HashMap;
import java.util.Map;

@Slf4j
@Service
@RequiredArgsConstructor
public class SnsService {

  private final SnsClient snsClient;
  private final ObjectMapper objectMapper = new ObjectMapper();

  @Value("${aws.sns.topic-arn}")
  private String topicArn;

  public void publishProcessMediaEvent(String mediaId, Integer width) {
    Map<String, Object> payload = new HashMap<>();
    payload.put("mediaId", mediaId);
    payload.put("width", width);

    publishEvent(EventType.PROCESS_MEDIA, payload);
    log.info("Published process media event for mediaId: {} with width: {}", mediaId, width);
  }

  public void publishDeleteMediaEvent(String mediaId) {
    Map<String, Object> payload = new HashMap<>();
    payload.put("mediaId", mediaId);

    publishEvent(EventType.DELETE_MEDIA, payload);
    log.info("Published delete media event for mediaId: {}", mediaId);
  }

  public void publishResizeMediaEvent(String mediaId, Integer width) {
    Map<String, Object> payload = new HashMap<>();
    payload.put("mediaId", mediaId);
    payload.put("width", width);

    publishEvent(EventType.RESIZE_MEDIA, payload);
    log.info("Published resize media event for mediaId: {} with width: {}", mediaId, width);
  }

  private void publishEvent(String eventType, Map<String, Object> payload) {
    try {
      Map<String, Object> message = new HashMap<>();
      message.put("type", eventType);
      message.put("payload", payload);

      String messageJson = objectMapper.writeValueAsString(message);

      PublishRequest request = PublishRequest.builder()
          .topicArn(topicArn)
          .message(messageJson)
          .build();

      snsClient.publish(request);
    } catch (JsonProcessingException e) {
      log.error("Failed to serialize event message: {}", e.getMessage());
      throw new RuntimeException("Failed to serialize event message", e);
    } catch (SnsException e) {
      log.error("Failed to publish event to SNS: {}", e.getMessage());
      throw e;
    }
  }
}
