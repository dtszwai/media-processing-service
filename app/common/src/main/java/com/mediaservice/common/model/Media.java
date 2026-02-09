package com.mediaservice.common.model;

import java.time.Instant;
import com.fasterxml.jackson.annotation.JsonIgnore;
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
  private String tenantId;
  private String userId;
  private Long size;
  /** Original filename as uploaded by user (for Content-Disposition header) */
  private String name;
  private String mimetype;
  private MediaType mediaType;
  private MediaStatus status;
  private Integer width;
  private OutputFormat outputFormat;
  private Instant createdAt;
  private Instant updatedAt;
  /** Timestamp when media was soft deleted, null if active */
  private Instant deletedAt;
  /** Optional webhook URL to call when processing completes */
  private String webhookUrl;
  /** Document metadata (PDF) */
  private Integer documentPageCount;
  private String documentTitle;
  private String documentAuthor;
  private String documentSubject;
  private String documentCreator;
  private String documentProducer;
  private Instant documentCreationDate;
  private Instant documentModifiedDate;
  private Long documentTextLength;
  private Boolean documentTextTruncated;

  /**
   * Returns the output format, defaulting to JPEG if not set.
   * Intended for image media types.
   *
   * @return the output format or JPEG as default
   */
  @JsonIgnore
  public OutputFormat getOutputFormatOrDefault() {
    return outputFormat != null ? outputFormat : OutputFormat.JPEG;
  }
}
