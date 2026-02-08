package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;

import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.*;

/**
 * DynamoDB service for analytics rollup operations.
 * Handles idempotency, batch writes, and data aggregation.
 */
public class AnalyticsDynamoDbService {
  private static final Logger logger = LoggerFactory.getLogger(AnalyticsDynamoDbService.class);

  private final DynamoDbClient client;
  private final String tableName;

  // Analytics PK prefixes
  private static final String ANALYTICS_PK_PREFIX = "ANALYTICS#TENANT#";
  private static final String ROLLUP_LOCK_PK = "ANALYTICS#ROLLUP#LOCK";
  private static final String TENANT_PK_PREFIX = "TENANT#";
  private static final String TENANT_METADATA_SK = "METADATA";

  public AnalyticsDynamoDbService() {
    this.client = AwsClientFactory.getDynamoDbClient();
    this.tableName = LambdaConfig.getInstance().getTableName();
  }

  /**
   * Constructor for testing with custom client.
   */
  AnalyticsDynamoDbService(DynamoDbClient client, String tableName) {
    this.client = client;
    this.tableName = tableName;
  }

  /**
   * Acquire an idempotency lock for a rollup operation.
   * Uses DynamoDB conditional write to ensure only one execution.
   *
   * @param rollupKey Unique key for the rollup (e.g., "daily-2024-01-15")
   * @return true if lock acquired (proceed with rollup), false if already
   *         processed
   */
  public boolean acquireIdempotencyLock(String rollupKey) {
    try {
      // TTL: 7 days from now
      long ttlEpoch = Instant.now().plus(7, ChronoUnit.DAYS).getEpochSecond();
      client.putItem(PutItemRequest.builder()
          .tableName(tableName)
          .item(Map.of(
              "PK", s(ROLLUP_LOCK_PK),
              "SK", s(rollupKey),
              "processedAt", s(Instant.now().toString()),
              "ttl", n(ttlEpoch)))
          .conditionExpression("attribute_not_exists(SK)")
          .build());
      logger.info("Acquired idempotency lock for rollup: {}", rollupKey);
      return true;
    } catch (ConditionalCheckFailedException e) {
      logger.info("Rollup already processed (idempotency check): {}", rollupKey);
      return false;
    } catch (Exception e) {
      logger.error("Failed to acquire idempotency lock for {}: {}", rollupKey, e.getMessage());
      throw e;
    }
  }

  /**
   * Persist analytics data to DynamoDB using batch write.
   *
   * @param period    Period type (DAILY, WEEKLY, MONTHLY, YEARLY)
   * @param periodKey Period key (e.g., "2024-01-15", "2024-W03", "2024-01",
   *                  "2024")
   * @param data      Map of mediaId to view count
   */
  public void persistAnalytics(String tenantId, String period, String periodKey, Map<String, Long> data) {
    if (data == null || data.isEmpty()) {
      logger.debug("No analytics data to persist for {}/{}", period, periodKey);
      return;
    }
    String pk = buildPk(tenantId, period, periodKey);
    var writeRequests = new ArrayList<WriteRequest>();
    int totalItems = 0;
    for (var entry : data.entrySet()) {
      String mediaId = entry.getKey();
      long viewCount = entry.getValue();
      if (mediaId == null || viewCount == 0) {
        continue;
      }
      var item = new HashMap<String, AttributeValue>();
      item.put("PK", s(pk));
      item.put("SK", s(mediaId));
      item.put("viewCount", n(viewCount));
      item.put("snapshotAt", s(Instant.now().toString()));
      writeRequests.add(WriteRequest.builder()
          .putRequest(PutRequest.builder().item(item).build())
          .build());
      // DynamoDB batch write limit is 25 items
      if (writeRequests.size() >= 25) {
        executeBatchWrite(writeRequests);
        totalItems += writeRequests.size();
        writeRequests.clear();
      }
    }

    // Write remaining items
    if (!writeRequests.isEmpty()) {
      executeBatchWrite(writeRequests);
      totalItems += writeRequests.size();
    }

    logger.info("Persisted {} analytics entries for tenant {} {}/{}", totalItems, tenantId, period, periodKey);
  }

  /**
   * Query analytics data for a specific period.
   *
   * @param period    Period type (DAILY, WEEKLY, MONTHLY, YEARLY)
   * @param periodKey Period key (e.g., "2024-01-15")
   * @return Map of mediaId to view count
   */
  public Map<String, Long> queryAnalytics(String tenantId, String period, String periodKey) {
    String pk = buildPk(tenantId, period, periodKey);
    var results = new HashMap<String, Long>();

    try {
      String exclusiveStartKey = null;
      do {
        var requestBuilder = QueryRequest.builder()
            .tableName(tableName)
            .keyConditionExpression("PK = :pk")
            .expressionAttributeValues(Map.of(":pk", s(pk)));

        if (exclusiveStartKey != null) {
          requestBuilder.exclusiveStartKey(Map.of(
              "PK", s(pk),
              "SK", s(exclusiveStartKey)));
        }

        var response = client.query(requestBuilder.build());

        for (var item : response.items()) {
          String mediaId = item.get("SK").s();
          long viewCount = Long.parseLong(item.get("viewCount").n());
          results.put(mediaId, viewCount);
        }

        // Check for pagination
        var lastKey = response.lastEvaluatedKey();
        exclusiveStartKey = (lastKey != null && !lastKey.isEmpty())
            ? lastKey.get("SK").s()
            : null;
      } while (exclusiveStartKey != null);

    } catch (Exception e) {
      logger.error("Failed to query analytics for {}/{}: {}", period, periodKey, e.getMessage());
    }

    return results;
  }

  /**
   * Aggregate analytics from multiple periods.
   *
   * @param period     Source period type (e.g., "DAILY")
   * @param periodKeys List of period keys to aggregate (e.g., ["2024-01-01",
   *                   "2024-01-02", ...])
   * @return Aggregated map of mediaId to total view count
   */
  public Map<String, Long> aggregateAnalytics(String tenantId, String period, List<String> periodKeys) {
    var aggregated = new HashMap<String, Long>();

    for (String periodKey : periodKeys) {
      var periodData = queryAnalytics(tenantId, period, periodKey);
      for (var entry : periodData.entrySet()) {
        aggregated.merge(entry.getKey(), entry.getValue(), Long::sum);
      }
    }

    logger.debug("Aggregated {} unique media items from {} periods for tenant {}",
        aggregated.size(), periodKeys.size(), tenantId);
    return aggregated;
  }

  /**
   * List all tenant IDs from the DynamoDB table.
   * Uses a scan with a prefix filter on PK to find tenant metadata records.
   */
  public Set<String> listTenantIds() {
    var tenants = new HashSet<String>();
    Map<String, AttributeValue> lastEvaluatedKey = null;

    try {
      do {
        var request = ScanRequest.builder()
            .tableName(tableName)
            .projectionExpression("PK, SK")
            .filterExpression("begins_with(PK, :pkPrefix) AND SK = :sk")
            .expressionAttributeValues(Map.of(
                ":pkPrefix", s(TENANT_PK_PREFIX),
                ":sk", s(TENANT_METADATA_SK)))
            .exclusiveStartKey(lastEvaluatedKey)
            .build();

        var response = client.scan(request);
        for (var item : response.items()) {
          var pk = item.get("PK");
          if (pk != null && pk.s() != null) {
            tenants.add(pk.s().replace(TENANT_PK_PREFIX, ""));
          }
        }
        lastEvaluatedKey = response.lastEvaluatedKey();
      } while (lastEvaluatedKey != null && !lastEvaluatedKey.isEmpty());
    } catch (Exception e) {
      logger.error("Failed to list tenants for analytics rollup: {}", e.getMessage());
    }

    return tenants;
  }

  private void executeBatchWrite(List<WriteRequest> writeRequests) {
    if (writeRequests.isEmpty()) {
      return;
    }
    try {
      var request = BatchWriteItemRequest.builder()
          .requestItems(Map.of(tableName, new ArrayList<>(writeRequests)))
          .build();

      var response = client.batchWriteItem(request);

      // Handle unprocessed items with exponential backoff
      var unprocessed = response.unprocessedItems();
      int retries = 0;
      while (unprocessed != null && !unprocessed.isEmpty() && retries < 3) {
        try {
          Thread.sleep(100L * (retries + 1));
        } catch (InterruptedException e) {
          Thread.currentThread().interrupt();
          throw new RuntimeException("Batch write interrupted", e);
        }

        var retryRequest = BatchWriteItemRequest.builder()
            .requestItems(unprocessed)
            .build();
        response = client.batchWriteItem(retryRequest);
        unprocessed = response.unprocessedItems();
        retries++;
      }

      if (unprocessed != null && !unprocessed.isEmpty()) {
        logger.warn("Failed to write {} items after {} retries",
            unprocessed.values().stream().mapToInt(List::size).sum(), retries);
      }
    } catch (Exception e) {
      logger.error("Batch write failed: {}", e.getMessage());
      throw e;
    }
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(long value) {
    return AttributeValue.builder().n(String.valueOf(value)).build();
  }

  private String buildPk(String tenantId, String period, String periodKey) {
    return ANALYTICS_PK_PREFIX + tenantId + "#VIEWS#" + period + "#" + periodKey;
  }
}
