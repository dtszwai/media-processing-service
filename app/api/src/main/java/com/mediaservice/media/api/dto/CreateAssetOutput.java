package com.mediaservice.media.api.dto;

import com.mediaservice.common.model.AssetOperation;
import jakarta.validation.constraints.NotNull;
import java.util.List;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class CreateAssetOutput {
  @NotNull(message = "operation is required")
  private AssetOperation operation;
  private String outputFormat;
  private Integer width;
  private String downloadName;
  private List<String> tags;
}
