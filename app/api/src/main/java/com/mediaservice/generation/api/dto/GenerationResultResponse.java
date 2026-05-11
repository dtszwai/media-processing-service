package com.mediaservice.generation.api.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.mediaservice.common.generation.GenerationStatus;
import java.time.Instant;
import java.util.List;
import lombok.Builder;
import lombok.Data;

@Data
@Builder
@JsonInclude(JsonInclude.Include.NON_NULL)
public class GenerationResultResponse {
  @JsonProperty("job_id")
  private String jobId;

  @JsonProperty("media_id")
  private String mediaId;

  private GenerationStatus status;

  @JsonProperty("image_url")
  private String imageUrl;

  @JsonProperty("audio_url")
  private String audioUrl;

  @JsonProperty("expires_at")
  private Instant expiresAt;

  private List<String> variants;

  @JsonProperty("is_ai_generated")
  private Boolean aiGenerated;
}
