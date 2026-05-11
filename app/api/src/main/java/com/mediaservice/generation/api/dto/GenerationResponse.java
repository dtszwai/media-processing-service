package com.mediaservice.generation.api.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.mediaservice.common.generation.GenerationStage;
import com.mediaservice.common.generation.GenerationStatus;
import java.time.Instant;
import lombok.Builder;
import lombok.Data;

@Data
@Builder
public class GenerationResponse {
  @JsonProperty("job_id")
  private String jobId;

  @JsonProperty("media_id")
  private String mediaId;

  private GenerationStatus status;
  private GenerationStage stage;

  @JsonProperty("estimated_wait_seconds")
  private Integer estimatedWaitSeconds;

  @JsonProperty("accepted_config")
  private AcceptedConfig acceptedConfig;

  private Admission admission;

  @JsonProperty("created_at")
  private Instant createdAt;

  @JsonProperty("updated_at")
  private Instant updatedAt;

  @JsonProperty("error_code")
  private String errorCode;

  @JsonProperty("error_message")
  private String errorMessage;

  @JsonProperty("is_ai_generated")
  private Boolean aiGenerated;

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record AcceptedConfig(
      @JsonProperty("resolution") String resolution,
      @JsonProperty("enhancement") Boolean enhancement
  ) {}

  @JsonInclude(JsonInclude.Include.NON_NULL)
  public record Admission(
      @JsonProperty("tier") String tier,
      @JsonProperty("admission_decision") String decision,
      @JsonProperty("admission_code") String code,
      @JsonProperty("retry_after_seconds") Integer retryAfterSeconds
  ) {}
}
