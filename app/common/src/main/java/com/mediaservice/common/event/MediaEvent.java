package com.mediaservice.common.event;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.mediaservice.common.model.EventType;
import com.mediaservice.common.model.OutputSpec;
import com.mediaservice.common.model.ProcessingJob;
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
    private String jobId;
    private String mediaId;
    private String tenantId;
    private String assetId;
    private String sourceAssetId;
    private String mediaType;
    private OutputSpec output;
  }

  public static MediaEvent forDelete(EventType eventType, String mediaId, String tenantId, String mediaType) {
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .mediaId(mediaId)
            .tenantId(tenantId)
            .mediaType(mediaType)
            .build())
        .build();
  }

  public static MediaEvent forJob(EventType eventType, ProcessingJob job, String mediaType) {
    OutputSpec output = OutputSpec.builder()
        .operation(job.getOperation())
        .outputFormat(job.getOutputFormat())
        .width(job.getWidth())
        .downloadName(job.getDownloadName())
        .tags(job.getTags())
        .build();
    return MediaEvent.builder()
        .type(eventType.getValue())
        .payload(MediaEventPayload.builder()
            .jobId(job.getJobId())
            .mediaId(job.getMediaId())
            .tenantId(job.getTenantId())
            .assetId(job.getAssetId())
            .sourceAssetId(job.getSourceAssetId())
            .mediaType(mediaType)
            .output(output)
            .build())
        .build();
  }
}
