package com.mediaservice.common.generation;

import java.time.Instant;
import java.util.Map;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class GenerationJob {
  private String jobId;
  private String mediaId;
  private String tenantId;
  private String userId;
  private String tier;
  private GenerationOutputType outputType;
  private GenerationStatus status;
  private GenerationStage currentStage;
  private String prompt;
  private String enhancedPrompt;
  private String model;
  private String resolution;
  private Long seed;
  private String webhookUrl;
  private String providerJobId;
  private String resultAssetId;
  private String resultContentType;
  private String resultExtension;
  private Long resultSizeBytes;
  private String errorCode;
  private String errorMessage;
  private Integer estimatedWaitSeconds;
  private Boolean aiGenerated;
  private Map<String, String> metadata;
  private Instant createdAt;
  private Instant updatedAt;
  private Instant completedAt;
}
