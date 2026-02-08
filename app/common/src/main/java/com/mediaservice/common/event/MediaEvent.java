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
    private String mediaType;
    private Integer width;
    private String outputFormat;
  }

  public static MediaEvent of(EventType eventType, String mediaId, String mediaType) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder().mediaId(mediaId).mediaType(mediaType).build())
        .build();
  }

  public static MediaEvent of(EventType eventType, String mediaId, String mediaType, Integer width, String outputFormat) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .mediaType(mediaType)
            .width(width)
            .outputFormat(outputFormat)
            .build())
        .build();
  }

  public static MediaEvent of(EventType eventType, String mediaId, String tenantId, String mediaType, Integer width, String outputFormat) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .tenantId(tenantId)
            .mediaType(mediaType)
            .width(width)
            .outputFormat(outputFormat)
            .build())
        .build();
  }

  public static MediaEvent of(EventType eventType, String mediaId, String tenantId, String mediaType) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .tenantId(tenantId)
            .mediaType(mediaType)
            .build())
        .build();
  }
}
