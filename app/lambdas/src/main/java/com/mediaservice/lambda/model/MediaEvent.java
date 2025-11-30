package com.mediaservice.lambda.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import lombok.AllArgsConstructor;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@NoArgsConstructor
@AllArgsConstructor
@JsonIgnoreProperties(ignoreUnknown = true)
public class MediaEvent {
  private String type;
  private MediaEventPayload payload;

  @Data
  @NoArgsConstructor
  @AllArgsConstructor
  @JsonIgnoreProperties(ignoreUnknown = true)
  public static class MediaEventPayload {
    private String mediaId;
    private Integer width;
    private String outputFormat;
  }
}
