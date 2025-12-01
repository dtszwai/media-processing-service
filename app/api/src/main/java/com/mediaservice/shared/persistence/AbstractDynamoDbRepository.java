package com.mediaservice.shared.persistence;

import lombok.extern.slf4j.Slf4j;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;
import software.amazon.awssdk.services.dynamodb.model.DeleteItemRequest;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;
import software.amazon.awssdk.services.dynamodb.model.QueryRequest;
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

/**
 * Abstract base class for DynamoDB repositories providing common operations.
 *
 * <p>
 * This class implements the Template Method pattern, providing common DynamoDB
 * operations while allowing subclasses to define entity-specific mapping logic.
 *
 * <p>
 * Subclasses must implement:
 * <ul>
 * <li>{@link #mapFromItem(Map)} - Convert DynamoDB item to entity</li>
 * <li>{@link #mapToItem(Object)} - Convert entity to DynamoDB item</li>
 * </ul>
 *
 * <p>
 * Usage example:
 *
 * <pre>
 * public class MediaRepository extends AbstractDynamoDbRepository&lt;Media&gt; {
 *   public MediaRepository(DynamoDbClient client, String tableName) {
 *     super(client, tableName);
 *   }
 *
 *   &#64;Override
 *   public Media mapFromItem(Map&lt;String, AttributeValue&gt; item) {
 *     return Media.builder()
 *         .mediaId(item.get("PK").s().replace("MEDIA#", ""))
 *         .build();
 *   }
 * }
 * </pre>
 *
 * @param <T> The entity type this repository manages
 */
@Slf4j
public abstract class AbstractDynamoDbRepository<T> implements DynamoDbRepository<T> {
  protected final DynamoDbClient dynamoDbClient;
  protected final String tableName;

  protected AbstractDynamoDbRepository(DynamoDbClient dynamoDbClient, String tableName) {
    this.dynamoDbClient = dynamoDbClient;
    this.tableName = tableName;
  }

  // ==================== Core CRUD Operations ====================

  @Override
  public Optional<T> findByKey(String pk, String sk) {
    var request = GetItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(pk, sk))
        .build();
    var response = dynamoDbClient.getItem(request);
    if (!response.hasItem() || response.item().isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(mapFromItem(response.item()));
  }

  @Override
  public void save(T entity) {
    var item = mapToItem(entity);
    var request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();
    dynamoDbClient.putItem(request);
    log.debug("Saved item to table {}", tableName);
  }

  @Override
  public void delete(String pk, String sk) {
    var request = DeleteItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(pk, sk))
        .build();
    dynamoDbClient.deleteItem(request);
    log.debug("Deleted item from table {}: PK={}, SK={}", tableName, pk, sk);
  }

  @Override
  public List<T> queryByPartitionKey(String pk, String keyExpression, int limit) {
    var request = QueryRequest.builder()
        .tableName(tableName)
        .keyConditionExpression(keyExpression)
        .expressionAttributeValues(Map.of(":pk", s(pk)))
        .limit(limit)
        .build();
    var response = dynamoDbClient.query(request);
    var results = new ArrayList<T>();
    for (var item : response.items()) {
      results.add(mapFromItem(item));
    }
    return results;
  }

  // ==================== Extended Operations ====================

  /**
   * Save an entity with a TTL (Time To Live).
   *
   * @param entity       The entity to save
   * @param ttl          Duration until the item expires
   * @param ttlAttribute The attribute name for the TTL field (e.g., "expiresAt")
   */
  public void saveWithTtl(T entity, Duration ttl, String ttlAttribute) {
    var item = new HashMap<>(mapToItem(entity));
    long expiresAtEpoch = Instant.now().plus(ttl).getEpochSecond();
    item.put(ttlAttribute, n(String.valueOf(expiresAtEpoch)));
    var request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();
    dynamoDbClient.putItem(request);
    log.debug("Saved item with TTL of {} to table {}", ttl, tableName);
  }

  /**
   * Update specific attributes of an item.
   *
   * @param pk               Partition key value
   * @param sk               Sort key value
   * @param updateExpression DynamoDB update expression
   * @param attributeNames   Expression attribute names
   * @param attributeValues  Expression attribute values
   */
  public void updateAttributes(String pk, String sk, String updateExpression,
      Map<String, String> attributeNames, Map<String, AttributeValue> attributeValues) {
    var requestBuilder = UpdateItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(pk, sk))
        .updateExpression(updateExpression)
        .expressionAttributeValues(attributeValues);
    if (attributeNames != null && !attributeNames.isEmpty()) {
      requestBuilder.expressionAttributeNames(attributeNames);
    }
    dynamoDbClient.updateItem(requestBuilder.build());
    log.debug("Updated item in table {}: PK={}, SK={}", tableName, pk, sk);
  }

  /**
   * Conditionally update an item.
   *
   * @param pk                  Partition key value
   * @param sk                  Sort key value
   * @param updateExpression    DynamoDB update expression
   * @param conditionExpression Condition that must be met for update
   * @param attributeNames      Expression attribute names
   * @param attributeValues     Expression attribute values
   * @return true if update succeeded, false if condition failed
   */
  public boolean updateConditionally(String pk, String sk, String updateExpression,
      String conditionExpression, Map<String, String> attributeNames,
      Map<String, AttributeValue> attributeValues) {
    try {
      var requestBuilder = UpdateItemRequest.builder()
          .tableName(tableName)
          .key(buildKey(pk, sk))
          .updateExpression(updateExpression)
          .conditionExpression(conditionExpression)
          .expressionAttributeValues(attributeValues);
      if (attributeNames != null && !attributeNames.isEmpty()) {
        requestBuilder.expressionAttributeNames(attributeNames);
      }
      dynamoDbClient.updateItem(requestBuilder.build());
      log.debug("Conditional update succeeded for PK={}, SK={}", pk, sk);
      return true;
    } catch (ConditionalCheckFailedException e) {
      log.debug("Conditional update failed for PK={}, SK={}", pk, sk);
      return false;
    }
  }

  /**
   * Query items using a GSI (Global Secondary Index).
   *
   * @param indexName        The GSI name
   * @param keyExpression    Key condition expression
   * @param attributeValues  Expression attribute values
   * @param scanIndexForward true for ascending order, false for descending
   * @param limit            Maximum number of items to return
   * @return List of mapped entities
   */
  public List<T> queryByIndex(String indexName, String keyExpression,
      Map<String, AttributeValue> attributeValues, boolean scanIndexForward, int limit) {
    var request = QueryRequest.builder()
        .tableName(tableName)
        .indexName(indexName)
        .keyConditionExpression(keyExpression)
        .expressionAttributeValues(attributeValues)
        .scanIndexForward(scanIndexForward)
        .limit(limit)
        .build();
    var response = dynamoDbClient.query(request);
    var results = new ArrayList<T>();
    for (var item : response.items()) {
      results.add(mapFromItem(item));
    }
    return results;
  }

  /**
   * Paginated query result container.
   */
  public record PagedResult<T>(List<T> items, String nextCursor, boolean hasMore) {
  }

  /**
   * Query with pagination support.
   *
   * @param indexName        The GSI name (null for main table)
   * @param keyExpression    Key condition expression
   * @param attributeValues  Expression attribute values
   * @param scanIndexForward true for ascending order, false for descending
   * @param limit            Page size
   * @param cursor           Base64-encoded cursor from previous page (null for
   *                         first page)
   * @param cursorAttributes Attributes to include in cursor
   * @return PagedResult containing items, next cursor, and hasMore flag
   */
  public PagedResult<T> queryPaginated(String indexName, String keyExpression,
      Map<String, AttributeValue> attributeValues, boolean scanIndexForward,
      int limit, String cursor, String... cursorAttributes) {
    var requestBuilder = QueryRequest.builder()
        .tableName(tableName)
        .keyConditionExpression(keyExpression)
        .expressionAttributeValues(attributeValues)
        .scanIndexForward(scanIndexForward)
        .limit(limit);
    if (indexName != null) {
      requestBuilder.indexName(indexName);
    }
    if (cursor != null && !cursor.isBlank()) {
      var exclusiveStartKey = decodeCursor(cursor, cursorAttributes);
      if (exclusiveStartKey != null) {
        requestBuilder.exclusiveStartKey(exclusiveStartKey);
      }
    }
    var response = dynamoDbClient.query(requestBuilder.build());
    var results = new ArrayList<T>();
    for (var item : response.items()) {
      results.add(mapFromItem(item));
    }
    String nextCursor = null;
    boolean hasMore = response.hasLastEvaluatedKey() && !response.lastEvaluatedKey().isEmpty();
    if (hasMore) {
      nextCursor = encodeCursor(response.lastEvaluatedKey(), cursorAttributes);
    }
    return new PagedResult<>(results, nextCursor, hasMore);
  }

  // ==================== Helper Methods ====================

  /**
   * Build a key map for DynamoDB operations.
   */
  protected Map<String, AttributeValue> buildKey(String pk, String sk) {
    return Map.of("PK", s(pk), "SK", s(sk));
  }

  /**
   * Create a String AttributeValue.
   */
  protected AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  /**
   * Create a Number AttributeValue.
   */
  protected AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }

  /**
   * Create a Boolean AttributeValue.
   */
  protected AttributeValue bool(boolean value) {
    return AttributeValue.builder().bool(value).build();
  }

  /**
   * Safely get a string value from an item.
   */
  protected String getString(Map<String, AttributeValue> item, String key) {
    var attr = item.get(key);
    return attr != null ? attr.s() : null;
  }

  /**
   * Safely get a number value as long from an item.
   */
  protected Long getLong(Map<String, AttributeValue> item, String key) {
    var attr = item.get(key);
    return attr != null ? Long.parseLong(attr.n()) : null;
  }

  /**
   * Safely get a number value as int from an item.
   */
  protected Integer getInt(Map<String, AttributeValue> item, String key) {
    var attr = item.get(key);
    return attr != null ? Integer.parseInt(attr.n()) : null;
  }

  /**
   * Safely get a boolean value from an item.
   */
  protected Boolean getBoolean(Map<String, AttributeValue> item, String key) {
    var attr = item.get(key);
    return attr != null ? attr.bool() : null;
  }

  /**
   * Parse an ISO timestamp string to Instant.
   */
  protected Instant getInstant(Map<String, AttributeValue> item, String key) {
    var value = getString(item, key);
    return value != null ? Instant.parse(value) : null;
  }

  /**
   * Encode cursor for pagination.
   */
  protected String encodeCursor(Map<String, AttributeValue> lastEvaluatedKey, String... attributes) {
    var parts = new StringBuilder();
    for (int i = 0; i < attributes.length; i++) {
      if (i > 0) {
        parts.append("|");
      }
      var attr = lastEvaluatedKey.get(attributes[i]);
      parts.append(attr != null ? attr.s() : "");
    }
    return Base64.getUrlEncoder().encodeToString(parts.toString().getBytes(StandardCharsets.UTF_8));
  }

  /**
   * Decode cursor for pagination.
   */
  protected Map<String, AttributeValue> decodeCursor(String cursor, String... attributes) {
    try {
      var decoded = new String(Base64.getUrlDecoder().decode(cursor), StandardCharsets.UTF_8);
      var parts = decoded.split("\\|", -1); // -1 to keep trailing empty strings
      if (parts.length != attributes.length) {
        log.warn("Invalid cursor format: expected {} parts, got {}", attributes.length, parts.length);
        return null;
      }
      var key = new HashMap<String, AttributeValue>();
      for (int i = 0; i < attributes.length; i++) {
        if (!parts[i].isEmpty()) {
          key.put(attributes[i], s(parts[i]));
        }
      }
      return key;
    } catch (IllegalArgumentException e) {
      log.warn("Failed to decode cursor: {}", cursor);
      return null;
    }
  }

  /**
   * Get the table name this repository operates on.
   */
  public String getTableName() {
    return tableName;
  }
}
