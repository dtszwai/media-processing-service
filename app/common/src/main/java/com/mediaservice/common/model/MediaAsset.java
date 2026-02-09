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
public class MediaAsset {
  private String assetId;
  private String mediaId;
  private String tenantId;
  private String sourceAssetId;
  private AssetType type;
  private List<String> tags;
  private AssetStatus status;
  private String outputFormat;
  private String mimetype;
  private Long size;
  private Integer width;
  private Integer height;
  private String downloadName;
  private AssetOperation operation;
  private Instant createdAt;
  private Instant updatedAt;
  private String errorMessage;
}
