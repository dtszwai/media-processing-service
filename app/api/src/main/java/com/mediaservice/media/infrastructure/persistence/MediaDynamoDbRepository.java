package com.mediaservice.media.infrastructure.persistence;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ScanRequest;

import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * DynamoDB repository for Media entities.
 *
 * <p>
 * Extends {@link AbstractDynamoDbRepository} to inherit common DynamoDB
 * operations
 * while providing media-specific functionality.
 *
 * <p>
 * Key schema:
 * <ul>
 * <li>PK: MEDIA#{mediaId}</li>
 * <li>SK: METADATA</li>
 * </ul>
 */
@Slf4j
@Repository
public class MediaDynamoDbRepository extends AbstractDynamoDbRepository<Media> {
  private static final int DEFAULT_PAGE_SIZE = 20;
  private static final String[] CURSOR_ATTRIBUTES = { "PK", "SK", "createdAt" };

  public MediaDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  // ==================== Entity Mapping ====================

  @Override
  public Media mapFromItem(Map<String, AttributeValue> item) {
    var pk = getString(item, "PK");
    var mediaId = pk.replace(StorageConstants.DYNAMO_PK_PREFIX, "");
    var builder = Media.builder()
        .mediaId(mediaId)
        .size(getLong(item, "size"))
        .name(getString(item, "name"))
        .mimetype(getString(item, "mimetype"))
        .status(MediaStatus.valueOf(getString(item, "status")))
        .width(getInt(item, "width"))
        .createdAt(getInstant(item, "createdAt"))
        .updatedAt(getInstant(item, "updatedAt"))
        .deletedAt(getInstant(item, "deletedAt"));
    var outputFormat = getString(item, "outputFormat");
    if (outputFormat != null) {
      builder.outputFormat(OutputFormat.fromString(outputFormat));
    }
    return builder.build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(Media media) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(StorageConstants.DYNAMO_PK_PREFIX + media.getMediaId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_METADATA));
    item.put("size", n(String.valueOf(media.getSize())));
    item.put("name", s(media.getName()));
    item.put("mimetype", s(media.getMimetype()));
    item.put("status", s(media.getStatus() != null ? media.getStatus().name() : MediaStatus.PENDING.name()));
    item.put("width", n(String.valueOf(media.getWidth())));
    item.put("outputFormat",
        s(media.getOutputFormat() != null ? media.getOutputFormat().getFormat() : OutputFormat.JPEG.getFormat()));
    item.put("createdAt", s(media.getCreatedAt() != null ? media.getCreatedAt().toString() : now.toString()));
    item.put("updatedAt", s(now.toString()));
    if (media.getDeletedAt() != null) {
      item.put("deletedAt", s(media.getDeletedAt().toString()));
    }
    return item;
  }

  // ==================== Media-Specific Operations ====================

  /**
   * Create a new media record.
   */
  public void createMedia(Media media) {
    createMedia(media, null);
  }

  /**
   * Create a new media record with optional TTL.
   *
   * @param media The media entity to create
   * @param ttl   Optional TTL for auto-expiration (e.g., for PENDING_UPLOAD
   *              records)
   */
  public void createMedia(Media media, Duration ttl) {
    if (ttl != null) {
      saveWithTtl(media, ttl, "expiresAt");
      log.info("Created media record for mediaId: {} with TTL of {} hours", media.getMediaId(), ttl.toHours());
    } else {
      save(media);
      log.info("Created media record for mediaId: {}", media.getMediaId());
    }
  }

  /**
   * Get a media record by ID.
   */
  public Optional<Media> getMedia(String mediaId) {
    return findByKey(StorageConstants.DYNAMO_PK_PREFIX + mediaId, StorageConstants.DYNAMO_SK_METADATA);
  }

  /**
   * Paginated result for media queries.
   */
  public record MediaPagedResult(List<Media> items, String nextCursor, boolean hasMore) {
  }

  /**
   * Get media records with pagination, excluding soft-deleted items.
   */
  public MediaPagedResult getMediaPaginated(String cursor, Integer limit) {
    int pageSize = (limit != null && limit > 0 && limit <= 100) ? limit : DEFAULT_PAGE_SIZE;
    var result = queryPaginated(
        StorageConstants.DYNAMO_GSI_SK_CREATED_AT,
        "SK = :sk",
        Map.of(":sk", s(StorageConstants.DYNAMO_SK_METADATA)),
        false, // newest first
        pageSize,
        cursor,
        CURSOR_ATTRIBUTES);
    // Filter out soft-deleted items
    var activeItems = result.items().stream()
        .filter(media -> media.getStatus() != MediaStatus.DELETED)
        .toList();
    log.info("Retrieved {} media records (hasMore={})", activeItems.size(), result.hasMore());
    return new MediaPagedResult(activeItems, result.nextCursor(), result.hasMore());
  }

  /**
   * Get all media records (deprecated - use pagination).
   */
  @Deprecated
  public List<Media> getAllMedia() {
    var mediaList = new ArrayList<Media>();
    var request = ScanRequest.builder()
        .tableName(tableName)
        .filterExpression("SK = :sk")
        .expressionAttributeValues(Map.of(":sk", s(StorageConstants.DYNAMO_SK_METADATA)))
        .build();
    var response = dynamoDbClient.scan(request);
    for (var item : response.items()) {
      mediaList.add(mapFromItem(item));
    }
    mediaList.sort((a, b) -> b.getCreatedAt().compareTo(a.getCreatedAt()));
    log.info("Retrieved {} media records", mediaList.size());
    return mediaList;
  }

  /**
   * Update the status of a media record.
   */
  public void updateStatus(String mediaId, MediaStatus status) {
    updateAttributes(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_METADATA,
        "SET #status = :status, updatedAt = :updatedAt",
        Map.of("#status", "status"),
        Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())));
    log.info("Updated status for mediaId: {} to {}", mediaId, status);
  }

  /**
   * Conditionally update the status of a media record.
   *
   * @return true if update succeeded, false if condition failed
   */
  public boolean updateStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus) {
    return updateStatusConditionally(mediaId, newStatus, expectedStatus, false);
  }

  /**
   * Conditionally update the status with option to clear TTL.
   *
   * @param clearTtl If true, removes the expiresAt attribute
   * @return true if update succeeded, false if condition failed
   */
  public boolean updateStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus,
      boolean clearTtl) {
    String updateExpression = clearTtl
        ? "SET #status = :newStatus, updatedAt = :updatedAt REMOVE expiresAt"
        : "SET #status = :newStatus, updatedAt = :updatedAt";
    boolean success = updateConditionally(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_METADATA,
        updateExpression,
        "#status = :expectedStatus",
        Map.of("#status", "status"),
        Map.of(
            ":newStatus", s(newStatus.name()),
            ":expectedStatus", s(expectedStatus.name()),
            ":updatedAt", s(Instant.now().toString())));
    if (success) {
      log.info("Updated status for mediaId: {} from {} to {}{}", mediaId, expectedStatus, newStatus,
          clearTtl ? " (TTL cleared)" : "");
    } else {
      log.warn("Conditional update failed for mediaId: {}, expected status: {}", mediaId, expectedStatus);
    }
    return success;
  }

  /**
   * Soft delete a media record by setting status to DELETED and deletedAt timestamp.
   * The record is retained for analytics/audit purposes; S3 files are deleted separately.
   */
  public void softDelete(String mediaId) {
    updateAttributes(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_METADATA,
        "SET #status = :status, deletedAt = :deletedAt, updatedAt = :updatedAt",
        Map.of("#status", "status"),
        Map.of(
            ":status", s(MediaStatus.DELETED.name()),
            ":deletedAt", s(Instant.now().toString()),
            ":updatedAt", s(Instant.now().toString())));
    log.info("Soft deleted media record for mediaId: {}", mediaId);
  }

  /**
   * Hard delete a media record (for compensation/cleanup only).
   */
  public void deleteMedia(String mediaId) {
    delete(StorageConstants.DYNAMO_PK_PREFIX + mediaId, StorageConstants.DYNAMO_SK_METADATA);
    log.info("Hard deleted media record for mediaId: {}", mediaId);
  }
}
