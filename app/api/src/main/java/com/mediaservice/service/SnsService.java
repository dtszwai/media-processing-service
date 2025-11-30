package com.mediaservice.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.event.MediaEvent;
import com.mediaservice.common.model.EventType;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sns.model.PublishRequest;

@Slf4j
@Service
@RequiredArgsConstructor
public class SnsService {

  private final SnsClient snsClient;
  private final ObjectMapper objectMapper;

  @Value("${aws.sns.topic-arn}")
  private String topicArn;

  public void publishProcessMediaEvent(String mediaId, Integer width, String outputFormat) {
    publishEvent(MediaEvent.of(EventType.PROCESS_MEDIA, mediaId, width, outputFormat));
    log.info("Published process media event for mediaId: {} with width: {}, outputFormat: {}", mediaId, width, outputFormat);
  }

  public void publishDeleteMediaEvent(String mediaId) {
    publishEvent(MediaEvent.of(EventType.DELETE_MEDIA, mediaId));
    log.info("Published delete media event for mediaId: {}", mediaId);
  }

  public void publishResizeMediaEvent(String mediaId, Integer width, String outputFormat) {
    publishEvent(MediaEvent.of(EventType.RESIZE_MEDIA, mediaId, width, outputFormat));
    log.info("Published resize media event for mediaId: {} with width: {}, outputFormat: {}", mediaId, width, outputFormat);
  }

  private void publishEvent(MediaEvent event) {
    try {
      String messageJson = objectMapper.writeValueAsString(event);
      PublishRequest request = PublishRequest.builder()
          .topicArn(topicArn)
          .message(messageJson)
          .build();
      snsClient.publish(request);
    } catch (JsonProcessingException e) {
      throw new IllegalStateException("Failed to serialize event message", e);
    }
  }
}
