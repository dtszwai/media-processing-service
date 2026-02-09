package com.mediaservice.common.model;

import java.util.List;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class OutputSpec {
  private AssetOperation operation;
  private String outputFormat;
  private Integer width;
  private String downloadName;
  private List<String> tags;
}
