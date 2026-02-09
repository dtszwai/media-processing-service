package com.mediaservice.media.api.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
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
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MediaAssetResponse {
  private String assetId;
  private String mediaId;
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
