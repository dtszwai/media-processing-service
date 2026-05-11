package com.mediaservice.common.model;

import java.time.Instant;
import com.fasterxml.jackson.annotation.JsonIgnore;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * Media entity representing a logical media item.
 *
 * <p>Processing outputs are stored as {@link MediaAsset} records. The media
 * entity tracks shared metadata (tenant, owner, original filename, document
 * metadata) and the original asset ID.
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
  /** Original filename as uploaded by user (for display and default download names) */
  private String name;
  private String mimetype;
  private MediaType mediaType;
  /** Origin of the media record. Legacy rows default to UPLOAD when the field is absent. */
  @Builder.Default
  private MediaSource source = MediaSource.UPLOAD;
  /** Summary status for overall media processing lifecycle. */
  private MediaStatus status;
  /** Original asset ID in the asset table */
  private String originalAssetId;
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

  @JsonIgnore
  public boolean isDeleted() {
    return deletedAt != null;
  }
}
