package com.mediaservice.common.model;

import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * Media entity representing an uploaded and processed media file.
 *
 * <p>The {@code name} field stores the original filename for display/download purposes,
 * while S3 keys are constructed using {@code mediaId} and variant names.
 *
 * @see com.mediaservice.common.constants.StorageConstants
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class Media {
  private String mediaId;
  private Long size;
  /** Original filename as uploaded by user (for Content-Disposition header) */
  private String name;
  private String mimetype;
  private MediaStatus status;
  private Integer width;
  private OutputFormat outputFormat;
  private Instant createdAt;
  private Instant updatedAt;
}
