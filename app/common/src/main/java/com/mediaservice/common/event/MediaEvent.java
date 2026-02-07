package com.mediaservice.common.event;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.mediaservice.common.model.EventType;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
@JsonIgnoreProperties(ignoreUnknown = true)
public class MediaEvent {
  private String type;
  private MediaEventPayload payload;

  @Data
  @Builder
  @NoArgsConstructor
  @AllArgsConstructor
  @JsonIgnoreProperties(ignoreUnknown = true)
  public static class MediaEventPayload {
    private String mediaId;
    private String tenantId;
    private Integer width;
    private String outputFormat;
  }

  public static MediaEvent of(EventType eventType, String mediaId) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder().mediaId(mediaId).build())
        .build();
  }

  public static MediaEvent of(EventType eventType, String mediaId, Integer width, String outputFormat) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .width(width)
            .outputFormat(outputFormat)
            .build())
        .build();
  }

  public static MediaEvent of(EventType eventType, String mediaId, String tenantId, Integer width, String outputFormat) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .tenantId(tenantId)
            .width(width)
            .outputFormat(outputFormat)
            .build())
        .build();
  }

  public static MediaEvent of(EventType eventType, String mediaId, String tenantId) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .tenantId(tenantId)
            .build())
        .build();
  }
}
