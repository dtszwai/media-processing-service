package com.mediaservice.common.model;

import java.time.Instant;
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
  private Long size;
  private String name;
  private String mimetype;
  private MediaStatus status;
  private Integer width;
  private OutputFormat outputFormat;
  private Instant createdAt;
  private Instant updatedAt;
}
