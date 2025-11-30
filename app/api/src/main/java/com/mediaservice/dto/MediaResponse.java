package com.mediaservice.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MediaResponse {
  private String mediaId;
  private Long size;
  private String name;
  private String mimetype;
  private MediaStatus status;
  private Integer width;
  private OutputFormat outputFormat;
  private Instant createdAt;
  private Instant updatedAt;
  private String message;
}
