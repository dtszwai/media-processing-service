package com.mediaservice.lambda.model;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class Media {
  private String mediaId;
  private String name;
  private MediaStatus status;
  private Integer width;
}
