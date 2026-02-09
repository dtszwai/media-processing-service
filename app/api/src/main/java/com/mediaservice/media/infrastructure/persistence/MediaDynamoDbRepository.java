package com.mediaservice.media.infrastructure.persistence;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;

import java.time.Duration;
import java.time.Instant;
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
        .tenantId(getString(item, StorageConstants.DYNAMO_ATTR_TENANT_ID))
        .userId(getString(item, StorageConstants.DYNAMO_ATTR_USER_ID))
        .size(getLong(item, "size"))
        .name(getString(item, "name"))
        .mimetype(getString(item, "mimetype"))
        .mediaType(MediaType.fromString(getString(item, "mediaType")))
        .status(MediaStatus.valueOf(getString(item, "status")))
        .width(getInt(item, "width"))
        .createdAt(getInstant(item, "createdAt"))
        .updatedAt(getInstant(item, "updatedAt"))
        .deletedAt(getInstant(item, "deletedAt"))
        .documentPageCount(getInt(item, "documentPageCount"))
        .documentTitle(getString(item, "documentTitle"))
        .documentAuthor(getString(item, "documentAuthor"))
        .documentSubject(getString(item, "documentSubject"))
        .documentCreator(getString(item, "documentCreator"))
        .documentProducer(getString(item, "documentProducer"))
        .documentCreationDate(getInstant(item, "documentCreationDate"))
        .documentModifiedDate(getInstant(item, "documentModifiedDate"))
        .documentTextLength(getLong(item, "documentTextLength"))
        .documentTextTruncated(getBoolean(item, "documentTextTruncated"));
    var outputFormat = getString(item, "outputFormat");
    if (outputFormat != null) {
      builder.outputFormat(OutputFormat.fromString(outputFormat));
    }
    var webhookUrl = getString(item, "webhookUrl");
    if (webhookUrl != null) {
      builder.webhookUrl(webhookUrl);
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
    if (media.getMediaType() != null) {
      item.put("mediaType", s(media.getMediaType().getValue()));
    }
    if (media.getWidth() != null) {
      item.put("width", n(String.valueOf(media.getWidth())));
    }
    if (media.getMediaType() == null || media.getMediaType() == MediaType.IMAGE) {
      item.put("outputFormat",
          s(media.getOutputFormat() != null ? media.getOutputFormat().getFormat() : OutputFormat.JPEG.getFormat()));
    }
    item.put("createdAt", s(media.getCreatedAt() != null ? media.getCreatedAt().toString() : now.toString()));
    item.put("updatedAt", s(now.toString()));
    if (media.getTenantId() != null) {
      item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(media.getTenantId()));
    }
    if (media.getUserId() != null) {
      item.put(StorageConstants.DYNAMO_ATTR_USER_ID, s(media.getUserId()));
    }
    if (media.getDeletedAt() != null) {
      item.put("deletedAt", s(media.getDeletedAt().toString()));
    }
    if (media.getWebhookUrl() != null) {
      item.put("webhookUrl", s(media.getWebhookUrl()));
    }
    if (media.getDocumentPageCount() != null) {
      item.put("documentPageCount", n(String.valueOf(media.getDocumentPageCount())));
    }
    if (media.getDocumentTitle() != null) {
      item.put("documentTitle", s(media.getDocumentTitle()));
    }
    if (media.getDocumentAuthor() != null) {
      item.put("documentAuthor", s(media.getDocumentAuthor()));
    }
    if (media.getDocumentSubject() != null) {
      item.put("documentSubject", s(media.getDocumentSubject()));
    }
    if (media.getDocumentCreator() != null) {
      item.put("documentCreator", s(media.getDocumentCreator()));
    }
    if (media.getDocumentProducer() != null) {
      item.put("documentProducer", s(media.getDocumentProducer()));
    }
    if (media.getDocumentCreationDate() != null) {
      item.put("documentCreationDate", s(media.getDocumentCreationDate().toString()));
    }
    if (media.getDocumentModifiedDate() != null) {
      item.put("documentModifiedDate", s(media.getDocumentModifiedDate().toString()));
    }
    if (media.getDocumentTextLength() != null) {
      item.put("documentTextLength", n(String.valueOf(media.getDocumentTextLength())));
    }
    if (media.getDocumentTextTruncated() != null) {
      item.put("documentTextTruncated", bool(media.getDocumentTextTruncated()));
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

  private record MediaFilterSpec(String expression, Map<String, String> names, Map<String, AttributeValue> values) {
  }

  private MediaFilterSpec buildMediaFilter(MediaType mediaType) {
    var names = new HashMap<String, String>();
    var values = new HashMap<String, AttributeValue>();

    names.put("#status", "status");
    values.put(":deleted", s(MediaStatus.DELETED.name()));

    String filter = "#status <> :deleted";

    if (mediaType != null) {
      names.put("#mediaType", "mediaType");
      values.put(":mediaType", s(mediaType.getValue()));
      if (mediaType == MediaType.IMAGE) {
        filter += " AND (attribute_not_exists(#mediaType) OR #mediaType = :mediaType)";
      } else {
        filter += " AND #mediaType = :mediaType";
      }
    }

    return new MediaFilterSpec(filter, names, values);
  }

  /**
   * Get media records with pagination, excluding soft-deleted items.
   */
  public MediaPagedResult getMediaPaginated(String cursor, Integer limit, MediaType mediaType) {
    int pageSize = (limit != null && limit > 0 && limit <= 100) ? limit : DEFAULT_PAGE_SIZE;
    var filter = buildMediaFilter(mediaType);
    var values = new HashMap<>(filter.values());
    values.put(":sk", s(StorageConstants.DYNAMO_SK_METADATA));
    values.put(":pkPrefix", s(StorageConstants.DYNAMO_PK_PREFIX));
    String filterExpression = "begins_with(PK, :pkPrefix) AND " + filter.expression();
    var result = queryMediaPaginated(
        StorageConstants.DYNAMO_GSI_SK_CREATED_AT,
        "SK = :sk",
        filterExpression,
        filter.names(),
        values,
        false, // newest first
        pageSize,
        cursor,
        CURSOR_ATTRIBUTES);
    log.info("Retrieved {} media records (mediaType={}, hasMore={})", result.items().size(),
        mediaType != null ? mediaType.getValue() : "any", result.hasMore());
    return new MediaPagedResult(result.items(), result.nextCursor(), result.hasMore());
  }

  /**
   * Get media records for a specific tenant with pagination, excluding soft-deleted items.
   */
  public MediaPagedResult getMediaPaginatedByTenant(String tenantId, String cursor, Integer limit, MediaType mediaType) {
    int pageSize = (limit != null && limit > 0 && limit <= 100) ? limit : DEFAULT_PAGE_SIZE;
    String[] tenantCursorAttributes = { StorageConstants.DYNAMO_ATTR_TENANT_ID, "createdAt", "PK", "SK" };
    var filter = buildMediaFilter(mediaType);
    var values = new HashMap<>(filter.values());
    values.put(":tenantId", s(tenantId));
    values.put(":pkPrefix", s(StorageConstants.DYNAMO_PK_PREFIX));
    values.put(":sk", s(StorageConstants.DYNAMO_SK_METADATA));
    String filterExpression = "begins_with(PK, :pkPrefix) AND SK = :sk AND " + filter.expression();
    var result = queryMediaPaginated(
        StorageConstants.DYNAMO_GSI_TENANT_CREATED_AT,
        "tenantId = :tenantId",
        filterExpression,
        filter.names(),
        values,
        false, // newest first
        pageSize,
        cursor,
        tenantCursorAttributes);
    log.info("Retrieved {} media records for tenant {} (mediaType={}, hasMore={})", result.items().size(), tenantId,
        mediaType != null ? mediaType.getValue() : "any", result.hasMore());
    return new MediaPagedResult(result.items(), result.nextCursor(), result.hasMore());
  }

  private PagedResult<Media> queryMediaPaginated(
      String indexName,
      String keyExpression,
      String filterExpression,
      Map<String, String> expressionAttributeNames,
      Map<String, AttributeValue> attributeValues,
      boolean scanIndexForward,
      int limit,
      String cursor,
      String[] cursorAttributes) {
    List<Media> collected = new java.util.ArrayList<>();
    String cursorValue = cursor;
    String nextCursor = null;
    boolean hasMore = false;
    int attempts = 0;

    while (collected.size() < limit && attempts < 5) {
      var result = queryPaginated(
          indexName,
          keyExpression,
          filterExpression,
          expressionAttributeNames,
          attributeValues,
          scanIndexForward,
          limit,
          cursorValue,
          cursorAttributes);

      if (result.items().isEmpty()) {
        if (!result.hasMore()) {
          hasMore = false;
          nextCursor = null;
          break;
        }
        cursorValue = result.nextCursor();
        hasMore = true;
        attempts++;
        continue;
      }

      boolean trimmed = false;
      for (int i = 0; i < result.items().size(); i++) {
        Media media = result.items().get(i);
        collected.add(media);
        if (collected.size() == limit) {
          trimmed = i < result.items().size() - 1;
          nextCursor = encodeCursorFromMedia(media, cursorAttributes);
          hasMore = trimmed || result.hasMore();
          break;
        }
      }

      if (collected.size() >= limit) {
        break;
      }

      if (result.hasMore()) {
        cursorValue = result.nextCursor();
        hasMore = true;
        attempts++;
      } else {
        hasMore = false;
        nextCursor = null;
        break;
      }
    }

    return new PagedResult<>(collected, hasMore ? nextCursor : null, hasMore);
  }

  private String encodeCursorFromMedia(Media media, String[] cursorAttributes) {
    var key = new HashMap<String, AttributeValue>();
    if (media.getTenantId() != null) {
      key.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(media.getTenantId()));
    }
    if (media.getCreatedAt() != null) {
      key.put("createdAt", s(media.getCreatedAt().toString()));
    }
    key.put("PK", s(StorageConstants.DYNAMO_PK_PREFIX + media.getMediaId()));
    key.put("SK", s(StorageConstants.DYNAMO_SK_METADATA));
    return encodeCursor(key, cursorAttributes);
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
   * Soft delete a media record by setting status to DELETED, deletedAt timestamp,
   * and expiresAt TTL for automatic hard deletion after the retention period.
   * The record is retained for analytics/audit purposes; S3 files are deleted separately.
   *
   * @param mediaId       The media ID to soft delete
   * @param retentionDays Retention period before DynamoDB auto-deletes the record
   */
  public void softDelete(String mediaId, Duration retentionDays) {
    var now = Instant.now();
    long expiresAtEpoch = now.plus(retentionDays).getEpochSecond();
    updateAttributes(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_METADATA,
        "SET #status = :status, deletedAt = :deletedAt, updatedAt = :updatedAt, expiresAt = :expiresAt",
        Map.of("#status", "status"),
        Map.of(
            ":status", s(MediaStatus.DELETED.name()),
            ":deletedAt", s(now.toString()),
            ":updatedAt", s(now.toString()),
            ":expiresAt", n(String.valueOf(expiresAtEpoch))));
    log.info("Soft deleted media record for mediaId: {} with TTL of {} days", mediaId, retentionDays.toDays());
  }

  /**
   * Revert a soft delete by restoring the original status and clearing deletedAt/expiresAt.
   * Used as compensation when event publishing fails after soft delete.
   */
  public void revertSoftDelete(String mediaId, MediaStatus originalStatus) {
    updateAttributes(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_METADATA,
        "SET #status = :status, updatedAt = :updatedAt REMOVE deletedAt, expiresAt",
        Map.of("#status", "status"),
        Map.of(
            ":status", s(originalStatus.name()),
            ":updatedAt", s(Instant.now().toString())));
    log.info("Reverted soft delete for mediaId: {}, restored status to {}", mediaId, originalStatus);
  }

  /**
   * Hard delete a media record (for compensation/cleanup only).
   */
  public void deleteMedia(String mediaId) {
    delete(StorageConstants.DYNAMO_PK_PREFIX + mediaId, StorageConstants.DYNAMO_SK_METADATA);
    log.info("Hard deleted media record for mediaId: {}", mediaId);
  }
}
