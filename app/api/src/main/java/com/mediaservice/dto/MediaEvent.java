package com.mediaservice.dto;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class MediaEvent {
  private String type;
  private MediaEventPayload payload;

  @Data
  @Builder
  @NoArgsConstructor
  @AllArgsConstructor
  public static class MediaEventPayload {
    private String mediaId;
    private Integer width;
    private String outputFormat;
  }

  public static MediaEvent of(String type, String mediaId) {
    return MediaEvent.builder()
        .type(type)
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .build())
        .build();
  }

  public static MediaEvent of(String type, String mediaId, Integer width) {
    return MediaEvent.builder()
        .type(type)
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .width(width)
            .build())
        .build();
  }

  public static MediaEvent of(String type, String mediaId, Integer width, String outputFormat) {
    return MediaEvent.builder()
        .type(type)
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .width(width)
            .outputFormat(outputFormat)
            .build())
        .build();
  }
}
