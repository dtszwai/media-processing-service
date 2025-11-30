package com.mediaservice.service;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;
import software.amazon.awssdk.services.dynamodb.model.QueryRequest;
import software.amazon.awssdk.services.dynamodb.model.ScanRequest;
import software.amazon.awssdk.services.dynamodb.model.UpdateItemRequest;

import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Base64;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

@Slf4j
@Service
@RequiredArgsConstructor
public class DynamoDbService {
  private static final int DEFAULT_PAGE_SIZE = 20;

  private final DynamoDbClient dynamoDbClient;

  @Value("${aws.dynamodb.table-name}")
  private String tableName;

  private Map<String, AttributeValue> buildKey(String mediaId) {
    return Map.of("PK", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId), "SK", s(StorageConstants.DYNAMO_SK_METADATA));
  }

  private Media mapToMedia(String mediaId, Map<String, AttributeValue> item) {
    var builder = Media.builder()
        .mediaId(mediaId)
        .size(Long.parseLong(item.get("size").n()))
        .name(item.get("name").s())
        .mimetype(item.get("mimetype").s())
        .status(MediaStatus.valueOf(item.get("status").s()))
        .width(Integer.parseInt(item.get("width").n()))
        .createdAt(Instant.parse(item.get("createdAt").s()))
        .updatedAt(Instant.parse(item.get("updatedAt").s()));
    if (item.containsKey("outputFormat")) {
      builder.outputFormat(OutputFormat.fromString(item.get("outputFormat").s()));
    }
    return builder.build();
  }

  public void createMedia(Media media) {
    createMedia(media, null);
  }

  public void createMedia(Media media, Duration ttl) {
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
    item.put("createdAt", s(now.toString()));
    item.put("updatedAt", s(now.toString()));

    // Set TTL for records that should auto-expire (e.g., PENDING_UPLOAD)
    if (ttl != null) {
      long expiresAtEpoch = now.plus(ttl).getEpochSecond();
      item.put("expiresAt", n(String.valueOf(expiresAtEpoch)));
    }

    var request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();
    dynamoDbClient.putItem(request);
    log.info("Created media record for mediaId: {}{}", media.getMediaId(),
        ttl != null ? " with TTL of " + ttl.toHours() + " hours" : "");
  }

  public Optional<Media> getMedia(String mediaId) {
    var request = GetItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(mediaId))
        .build();
    var response = dynamoDbClient.getItem(request);
    if (!response.hasItem() || response.item().isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(mapToMedia(mediaId, response.item()));
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }

  public record PagedResult(List<Media> items, String nextCursor, boolean hasMore) {
  }

  public PagedResult getMediaPaginated(String cursor, Integer limit) {
    int pageSize = (limit != null && limit > 0 && limit <= 100) ? limit : DEFAULT_PAGE_SIZE;
    var requestBuilder = QueryRequest.builder()
        .tableName(tableName)
        .indexName(StorageConstants.DYNAMO_GSI_SK_CREATED_AT)
        .keyConditionExpression("SK = :sk")
        .expressionAttributeValues(Map.of(":sk", s(StorageConstants.DYNAMO_SK_METADATA)))
        .scanIndexForward(false)
        .limit(pageSize);
    if (cursor != null && !cursor.isBlank()) {
      var exclusiveStartKey = decodeCursor(cursor);
      if (exclusiveStartKey != null) {
        requestBuilder.exclusiveStartKey(exclusiveStartKey);
      }
    }
    var response = dynamoDbClient.query(requestBuilder.build());
    var mediaList = new ArrayList<Media>();
    for (var item : response.items()) {
      var pk = item.get("PK").s();
      var mediaId = pk.replace(StorageConstants.DYNAMO_PK_PREFIX, "");
      mediaList.add(mapToMedia(mediaId, item));
    }
    String nextCursor = null;
    boolean hasMore = response.hasLastEvaluatedKey() && !response.lastEvaluatedKey().isEmpty();
    if (hasMore) {
      nextCursor = encodeCursor(response.lastEvaluatedKey());
    }
    log.info("Retrieved {} media records (hasMore={})", mediaList.size(), hasMore);
    return new PagedResult(mediaList, nextCursor, hasMore);
  }

  private String encodeCursor(Map<String, AttributeValue> lastEvaluatedKey) {
    var pk = lastEvaluatedKey.get("PK").s();
    var sk = lastEvaluatedKey.get("SK").s();
    var createdAt = lastEvaluatedKey.get("createdAt").s();
    var cursorData = pk + "|" + sk + "|" + createdAt;
    return Base64.getUrlEncoder().encodeToString(cursorData.getBytes(StandardCharsets.UTF_8));
  }

  private Map<String, AttributeValue> decodeCursor(String cursor) {
    try {
      var decoded = new String(Base64.getUrlDecoder().decode(cursor), StandardCharsets.UTF_8);
      var parts = decoded.split("\\|");
      if (parts.length != 3) {
        log.warn("Invalid cursor format: {}", cursor);
        return null;
      }
      return Map.of(
          "PK", s(parts[0]),
          "SK", s(parts[1]),
          "createdAt", s(parts[2]));
    } catch (IllegalArgumentException e) {
      log.warn("Failed to decode cursor: {}", cursor);
      return null;
    }
  }

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
      var pk = item.get("PK").s();
      var mediaId = pk.replace(StorageConstants.DYNAMO_PK_PREFIX, "");
      mediaList.add(mapToMedia(mediaId, item));
    }
    mediaList.sort((a, b) -> b.getCreatedAt().compareTo(a.getCreatedAt()));
    log.info("Retrieved {} media records", mediaList.size());
    return mediaList;
  }

  public void updateStatus(String mediaId, MediaStatus status) {
    var request = UpdateItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(mediaId))
        .updateExpression("SET #status = :status, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())))
        .build();
    dynamoDbClient.updateItem(request);
    log.info("Updated status for mediaId: {} to {}", mediaId, status);
  }

  public boolean updateStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus) {
    return updateStatusConditionally(mediaId, newStatus, expectedStatus, false);
  }

  public boolean updateStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus, boolean clearTtl) {
    try {
      String updateExpression = clearTtl
          ? "SET #status = :newStatus, updatedAt = :updatedAt REMOVE expiresAt"
          : "SET #status = :newStatus, updatedAt = :updatedAt";
      var request = UpdateItemRequest.builder()
          .tableName(tableName)
          .key(buildKey(mediaId))
          .updateExpression(updateExpression)
          .conditionExpression("#status = :expectedStatus")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":newStatus", s(newStatus.name()),
              ":expectedStatus", s(expectedStatus.name()),
              ":updatedAt", s(Instant.now().toString())))
          .build();
      dynamoDbClient.updateItem(request);
      log.info("Updated status for mediaId: {} from {} to {}{}", mediaId, expectedStatus, newStatus,
          clearTtl ? " (TTL cleared)" : "");
      return true;
    } catch (ConditionalCheckFailedException e) {
      log.warn("Conditional update failed for mediaId: {}, expected status: {}", mediaId, expectedStatus);
      return false;
    }
  }

  public void deleteMedia(String mediaId) {
    dynamoDbClient.deleteItem(builder -> builder.tableName(tableName).key(buildKey(mediaId)));
    log.info("Deleted media record for mediaId: {}", mediaId);
  }
}
