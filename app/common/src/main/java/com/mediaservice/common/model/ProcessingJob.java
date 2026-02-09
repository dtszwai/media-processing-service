package com.mediaservice.common.model;

import java.time.Instant;
import java.util.List;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class ProcessingJob {
  private String jobId;
  private String mediaId;
  private String tenantId;
  private String assetId;
  private String sourceAssetId;
  private AssetOperation operation;
  private String outputFormat;
  private Integer width;
  private String downloadName;
  private List<String> tags;
  private ProcessingJobStatus status;
  private Integer attempts;
  private String errorMessage;
  private Instant createdAt;
  private Instant updatedAt;
}
