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
public class GenerationArtifact {
  private String artifactId;
  private String jobId;
  private String mediaId;
  private String assetId;
  private String tenantId;
  private String artifactType;
  private String uri;
  private String contentType;
  private String extension;
  private Long sizeBytes;
  private String checksum;
  private Map<String, String> metadata;
  private Instant createdAt;
  private Instant expiresAt;
}
