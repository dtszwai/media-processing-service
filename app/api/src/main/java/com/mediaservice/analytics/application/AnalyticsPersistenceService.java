package com.mediaservice.analytics.application;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.shared.config.properties.AnalyticsProperties;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

import java.time.Duration;
import java.time.Instant;
import java.time.LocalDate;
import java.time.YearMonth;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.util.*;
import java.util.concurrent.TimeUnit;

/**
 * Service for persisting analytics data from Redis to DynamoDB and S3.
 *
 * <p>
 * Implements a scheduled snapshot strategy:
 * <ul>
 * <li>Real-time data stays in Redis (fast reads/writes)</li>
 * <li>Daily snapshots are persisted to DynamoDB (durable storage)</li>
 * <li>Monthly archives are stored in S3 (long-term storage)</li>
 * <li>On startup, can restore Redis from DynamoDB if needed</li>
 * </ul>
 *
 * <p>
 * DynamoDB Schema for Analytics:
 * <ul>
 * <li>PK: {@code ANALYTICS#VIEWS#{period}#{date}} (e.g.,
 * ANALYTICS#VIEWS#DAILY#2024-01-15)</li>
 * <li>SK: {@code {mediaId}}</li>
 * <li>viewCount: Number of views</li>
 * <li>snapshotAt: Timestamp of snapshot</li>
 * <li>ttl: Expiration timestamp for automatic cleanup (optional)</li>
 * </ul>
 *
 * <p>
 * S3 Archive Format:
 * <ul>
 * <li>Path: {@code analytics/{year}/{month}/analytics-{year}-{month}.json}</li>
 * <li>Contains aggregated monthly data as JSON</li>
 * </ul>
 */
@Service
@Slf4j
public class AnalyticsPersistenceService {
  private final DynamoDbClient dynamoDbClient;
  private final S3Client s3Client;
  private final StringRedisTemplate redisTemplate;
  private final AnalyticsProperties analyticsProperties;
  private final ObjectMapper objectMapper;
  private final String bucketName;

  public AnalyticsPersistenceService(
      DynamoDbClient dynamoDbClient,
      S3Client s3Client,
      StringRedisTemplate redisTemplate,
      AnalyticsProperties analyticsProperties,
      ObjectMapper objectMapper,
      @Value("${aws.s3.bucket-name}") String bucketName) {
    this.dynamoDbClient = dynamoDbClient;
    this.s3Client = s3Client;
    this.redisTemplate = redisTemplate;
    this.analyticsProperties = analyticsProperties;
    this.objectMapper = objectMapper;
    this.bucketName = bucketName;
  }

  private static final String TABLE_NAME = "media";
  private static final String ANALYTICS_PK_PREFIX = "ANALYTICS#VIEWS#";

  private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;

  // Redis key prefixes (must match AnalyticsService)
  private static final String VIEWS_DAILY_PREFIX = "views:daily:";
  private static final String VIEWS_TOTAL_KEY = "views:total";

  // Distributed lock for write-behind (prevents multiple instances from writing)
  private static final String WRITE_BEHIND_LOCK_KEY = "analytics:writebehind:lock";
  private static final Duration WRITE_BEHIND_LOCK_TTL = Duration.ofMinutes(2);

  /**
   * Write-behind task: Persist current day's analytics from Redis to DynamoDB.
   *
   * <p>
   * Runs every 5 minutes to ensure data durability. If Redis crashes,
   * we lose at most 5 minutes of data instead of a full day.
   *
   * <p>
   * Uses distributed locking (Redis SETNX) to prevent multiple instances
   * from writing simultaneously.
   */
  @Scheduled(fixedRate = 300000) // 5 minutes
  public void writeBehindPersistence() {
    if (!analyticsProperties.isEnabled() || !analyticsProperties.getPersistence().isEnabled()) {
      return;
    }
    // Try to acquire distributed lock
    if (!acquireWriteBehindLock()) {
      log.debug("Write-behind lock held by another instance, skipping");
      return;
    }
    try {
      var today = LocalDate.now();
      log.debug("Starting write-behind persistence for {}", today);
      // Persist today's data (upsert pattern)
      snapshotDayToDb(today);
      log.debug("Write-behind persistence completed for {}", today);
    } catch (Exception e) {
      log.error("Write-behind persistence failed: {}", e.getMessage(), e);
    } finally {
      releaseWriteBehindLock();
    }
  }

  /**
   * Acquire a distributed lock using Redis SETNX.
   * Returns true if lock was acquired, false if another instance holds it.
   */
  private boolean acquireWriteBehindLock() {
    try {
      Boolean acquired = redisTemplate.opsForValue().setIfAbsent(
          WRITE_BEHIND_LOCK_KEY,
          "locked",
          WRITE_BEHIND_LOCK_TTL);
      return Boolean.TRUE.equals(acquired);
    } catch (Exception e) {
      log.warn("Failed to acquire write-behind lock: {}", e.getMessage());
      return false;
    }
  }

  /**
   * Release the distributed lock.
   */
  private void releaseWriteBehindLock() {
    try {
      redisTemplate.delete(WRITE_BEHIND_LOCK_KEY);
    } catch (Exception e) {
      log.warn("Failed to release write-behind lock: {}", e.getMessage());
    }
  }

  /**
   * Snapshot yesterday's daily analytics to DynamoDB.
   *
   * <p>
   * NOTE: This is now handled by the AnalyticsRollupHandler Lambda
   * via EventBridge scheduled triggers. This method is kept for:
   * <ul>
   * <li>Manual invocation via admin endpoints</li>
   * <li>Backward compatibility during migration</li>
   * <li>Testing purposes</li>
   * </ul>
   */
  public void snapshotDailyAnalytics() {
    if (!analyticsProperties.isEnabled() || !analyticsProperties.getPersistence().isEnabled()) {
      return;
    }
    try {
      var yesterday = LocalDate.now().minusDays(1);
      snapshotDayToDb(yesterday);
      log.info("Daily analytics snapshot completed for {}", yesterday);
    } catch (Exception e) {
      log.error("Failed to snapshot daily analytics: {}", e.getMessage(), e);
    }
  }

  /**
   * Snapshot a specific day's analytics from Redis to DynamoDB.
   *
   * @param date The date to snapshot
   */
  public void snapshotDayToDb(LocalDate date) {
    String redisKey = VIEWS_DAILY_PREFIX + date.format(DATE_FORMATTER);
    String dynamoPk = ANALYTICS_PK_PREFIX + "DAILY#" + date.format(DATE_FORMATTER);
    try {
      // Get all entries from Redis sorted set
      var entries = redisTemplate.opsForZSet().rangeWithScores(redisKey, 0, -1);
      if (entries == null || entries.isEmpty()) {
        log.debug("No analytics data to snapshot for {}", date);
        return;
      }

      var persistenceConfig = analyticsProperties.getPersistence();
      boolean ttlEnabled = persistenceConfig.isTtlEnabled();

      // Calculate TTL only if enabled
      Long ttlEpoch = null;
      if (ttlEnabled) {
        int retentionDays = persistenceConfig.getDailyRetentionDays();
        ttlEpoch = date.plusDays(retentionDays).atStartOfDay(ZoneOffset.UTC).toEpochSecond();
      }

      // Batch write to DynamoDB
      var writeRequests = new ArrayList<WriteRequest>();
      for (var entry : entries) {
        String mediaId = entry.getValue();
        long viewCount = entry.getScore() != null ? entry.getScore().longValue() : 0;
        if (mediaId == null || viewCount == 0) {
          continue;
        }
        var item = new HashMap<String, AttributeValue>();
        item.put("PK", AttributeValue.builder().s(dynamoPk).build());
        item.put("SK", AttributeValue.builder().s(mediaId).build());
        item.put("viewCount", AttributeValue.builder().n(String.valueOf(viewCount)).build());
        item.put("snapshotAt", AttributeValue.builder().s(Instant.now().toString()).build());

        // Only add TTL if enabled
        if (ttlEpoch != null) {
          item.put("ttl", AttributeValue.builder().n(String.valueOf(ttlEpoch)).build());
        }

        writeRequests.add(WriteRequest.builder()
            .putRequest(PutRequest.builder().item(item).build())
            .build());
        // DynamoDB batch write limit is 25 items
        if (writeRequests.size() >= 25) {
          executeBatchWrite(writeRequests);
          writeRequests.clear();
        }
      }
      // Write remaining items
      if (!writeRequests.isEmpty()) {
        executeBatchWrite(writeRequests);
      }
      log.info("Persisted {} analytics entries for {} to DynamoDB (TTL: {})",
          entries.size(), date, ttlEnabled ? "enabled" : "disabled");
    } catch (Exception e) {
      log.error("Failed to snapshot analytics for {}: {}", date, e.getMessage(), e);
      throw e;
    }
  }

  /**
   * Snapshot monthly aggregated analytics to DynamoDB.
   * Called at the end of each month.
   *
   * @param yearMonth The year-month to snapshot (e.g., "2024-01")
   * @return Map of mediaId to view count (for S3 archival)
   */
  public Map<String, Long> snapshotMonthToDb(String yearMonth) {
    var dynamoPk = ANALYTICS_PK_PREFIX + "MONTHLY#" + yearMonth;
    var aggregatedViews = new HashMap<String, Long>();
    try {
      // Query all daily snapshots for the month
      var monthStart = LocalDate.parse(yearMonth + "-01");
      var monthEnd = monthStart.plusMonths(1).minusDays(1);
      for (var date = monthStart; !date.isAfter(monthEnd); date = date.plusDays(1)) {
        var dailyPk = ANALYTICS_PK_PREFIX + "DAILY#" + date.format(DATE_FORMATTER);
        var response = dynamoDbClient.query(QueryRequest.builder()
            .tableName(TABLE_NAME)
            .keyConditionExpression("PK = :pk")
            .expressionAttributeValues(Map.of(":pk", AttributeValue.builder().s(dailyPk).build()))
            .build());
        for (var item : response.items()) {
          var mediaId = item.get("SK").s();
          long viewCount = Long.parseLong(item.get("viewCount").n());
          aggregatedViews.merge(mediaId, viewCount, Long::sum);
        }
      }
      if (aggregatedViews.isEmpty()) {
        log.debug("No monthly analytics data to snapshot for {}", yearMonth);
        return aggregatedViews;
      }

      var persistenceConfig = analyticsProperties.getPersistence();
      boolean ttlEnabled = persistenceConfig.isTtlEnabled();

      // Calculate TTL only if enabled
      Long ttlEpoch = null;
      if (ttlEnabled) {
        var monthlyRetentionDays = persistenceConfig.getMonthlyRetentionDays();
        ttlEpoch = monthStart.plusDays(monthlyRetentionDays).atStartOfDay(ZoneOffset.UTC).toEpochSecond();
      }

      // Write aggregated monthly data
      var writeRequests = new ArrayList<WriteRequest>();
      for (var entry : aggregatedViews.entrySet()) {
        var item = new HashMap<String, AttributeValue>();
        item.put("PK", AttributeValue.builder().s(dynamoPk).build());
        item.put("SK", AttributeValue.builder().s(entry.getKey()).build());
        item.put("viewCount", AttributeValue.builder().n(String.valueOf(entry.getValue())).build());
        item.put("snapshotAt", AttributeValue.builder().s(Instant.now().toString()).build());

        // Only add TTL if enabled
        if (ttlEpoch != null) {
          item.put("ttl", AttributeValue.builder().n(String.valueOf(ttlEpoch)).build());
        }

        writeRequests.add(WriteRequest.builder()
            .putRequest(PutRequest.builder().item(item).build())
            .build());
        if (writeRequests.size() >= 25) {
          executeBatchWrite(writeRequests);
          writeRequests.clear();
        }
      }
      if (!writeRequests.isEmpty()) {
        executeBatchWrite(writeRequests);
      }
      log.info("Persisted monthly analytics for {}: {} media items (TTL: {})",
          yearMonth, aggregatedViews.size(), ttlEnabled ? "enabled" : "disabled");
    } catch (Exception e) {
      log.error("Failed to snapshot monthly analytics for {}: {}", yearMonth, e.getMessage(), e);
    }
    return aggregatedViews;
  }

  /**
   * Restore Redis analytics data from DynamoDB.
   * Used on startup if Redis data is lost.
   *
   * @param date The date to restore
   */
  public void restoreFromDb(LocalDate date) {
    String dynamoPk = ANALYTICS_PK_PREFIX + "DAILY#" + date.format(DATE_FORMATTER);
    String redisKey = VIEWS_DAILY_PREFIX + date.format(DATE_FORMATTER);
    try {
      var response = dynamoDbClient.query(QueryRequest.builder()
          .tableName(TABLE_NAME)
          .keyConditionExpression("PK = :pk")
          .expressionAttributeValues(Map.of(":pk", AttributeValue.builder().s(dynamoPk).build()))
          .build());
      if (response.items().isEmpty()) {
        log.debug("No persisted analytics data found for {}", date);
        return;
      }
      // Restore to Redis
      for (var item : response.items()) {
        String mediaId = item.get("SK").s();
        long viewCount = Long.parseLong(item.get("viewCount").n());
        redisTemplate.opsForZSet().add(redisKey, mediaId, viewCount);
        // Also update total views
        redisTemplate.opsForZSet().incrementScore(VIEWS_TOTAL_KEY, mediaId, viewCount);
      }
      // Set TTL on restored key
      int retentionDays = analyticsProperties.getPersistence().getDailyRetentionDays();
      redisTemplate.expire(redisKey, retentionDays, TimeUnit.DAYS);
      log.info("Restored {} analytics entries for {} from DynamoDB", response.items().size(), date);
    } catch (Exception e) {
      log.error("Failed to restore analytics for {}: {}", date, e.getMessage(), e);
    }
  }

  /**
   * Aggregate daily analytics over a date range from DynamoDB.
   * Used for calculating weekly/monthly/yearly views at read-time (Roll-Up
   * Pattern).
   *
   * @param startDate Start date (inclusive)
   * @param endDate   End date (inclusive)
   * @param limit     Maximum number of results (top N media)
   * @return Map of mediaId to aggregated view count, sorted by views descending
   */
  public Map<String, Long> aggregateDailyAnalytics(LocalDate startDate, LocalDate endDate, int limit) {
    var aggregated = new HashMap<String, Long>();
    try {
      for (var date = startDate; !date.isAfter(endDate); date = date.plusDays(1)) {
        String dynamoPk = ANALYTICS_PK_PREFIX + "DAILY#" + date.format(DATE_FORMATTER);
        var response = dynamoDbClient.query(QueryRequest.builder()
            .tableName(TABLE_NAME)
            .keyConditionExpression("PK = :pk")
            .expressionAttributeValues(Map.of(":pk", AttributeValue.builder().s(dynamoPk).build()))
            .build());
        for (var item : response.items()) {
          var mediaId = item.get("SK").s();
          long viewCount = Long.parseLong(item.get("viewCount").n());
          aggregated.merge(mediaId, viewCount, Long::sum);
        }
      }

      // Sort by view count descending and limit results
      return aggregated.entrySet().stream()
          .sorted((a, b) -> Long.compare(b.getValue(), a.getValue()))
          .limit(limit)
          .collect(java.util.stream.Collectors.toMap(
              Map.Entry::getKey,
              Map.Entry::getValue,
              (e1, e2) -> e1,
              LinkedHashMap::new));
    } catch (Exception e) {
      log.error("Failed to aggregate daily analytics from {} to {}: {}",
          startDate, endDate, e.getMessage());
      return Collections.emptyMap();
    }
  }

  /**
   * Get historical analytics for a specific date from DynamoDB.
   *
   * @param period  The period type (DAILY, MONTHLY, YEARLY)
   * @param dateKey The date key (e.g., "2024-01-15" for daily, "2024-01" for
   *                monthly)
   * @param limit   Maximum number of results
   * @return Map of mediaId to view count
   */
  public Map<String, Long> getHistoricalAnalytics(String period, String dateKey, int limit) {
    String dynamoPk = ANALYTICS_PK_PREFIX + period + "#" + dateKey;
    var results = new LinkedHashMap<String, Long>();
    try {
      var response = dynamoDbClient.query(QueryRequest.builder()
          .tableName(TABLE_NAME)
          .keyConditionExpression("PK = :pk")
          .expressionAttributeValues(Map.of(":pk", AttributeValue.builder().s(dynamoPk).build()))
          .limit(limit)
          .build());
      // Sort by view count descending
      response.items().stream()
          .sorted((a, b) -> Long.compare(
              Long.parseLong(b.get("viewCount").n()),
              Long.parseLong(a.get("viewCount").n())))
          .limit(limit)
          .forEach(item -> results.put(
              item.get("SK").s(),
              Long.parseLong(item.get("viewCount").n())));
    } catch (Exception e) {
      log.error("Failed to get historical analytics for {}/{}: {}", period, dateKey, e.getMessage());
    }
    return results;
  }

  /**
   * Archive last month's analytics to S3.
   *
   * <p>
   * NOTE: This is now handled by the AnalyticsRollupHandler Lambda
   * via EventBridge scheduled triggers. This method is kept for:
   * <ul>
   * <li>Manual invocation via admin endpoints</li>
   * <li>Backward compatibility during migration</li>
   * <li>Testing purposes</li>
   * </ul>
   */
  public void archiveMonthlyAnalyticsToS3() {
    var s3Config = analyticsProperties.getPersistence().getS3Archive();
    if (!analyticsProperties.isEnabled() || !analyticsProperties.getPersistence().isEnabled()
        || !s3Config.isEnabled()) {
      return;
    }

    try {
      // Archive last month's data
      var lastMonth = YearMonth.now().minusMonths(1);
      String yearMonth = lastMonth.format(DateTimeFormatter.ofPattern("yyyy-MM"));

      // First snapshot to DynamoDB, then archive to S3
      var monthlyData = snapshotMonthToDb(yearMonth);
      if (!monthlyData.isEmpty()) {
        archiveToS3(yearMonth, monthlyData);
      }

      log.info("Monthly analytics archive completed for {}", yearMonth);
    } catch (Exception e) {
      log.error("Failed to archive monthly analytics to S3: {}", e.getMessage(), e);
    }
  }

  /**
   * Archive analytics data to S3 for long-term storage.
   *
   * @param yearMonth The year-month (e.g., "2024-01")
   * @param data      Map of mediaId to view count
   */
  public void archiveToS3(String yearMonth, Map<String, Long> data) {
    var s3Config = analyticsProperties.getPersistence().getS3Archive();
    if (!s3Config.isEnabled()) {
      log.debug("S3 archival is disabled");
      return;
    }

    try {
      // Build S3 key: analytics/2024/01/analytics-2024-01.json
      String[] parts = yearMonth.split("-");
      String year = parts[0];
      String month = parts[1];
      String s3Key = String.format("%s%s/%s/analytics-%s.json",
          s3Config.getPrefix(), year, month, yearMonth);

      // Create archive record
      var archiveRecord = new AnalyticsArchive(
          yearMonth,
          Instant.now().toString(),
          data.size(),
          data.values().stream().mapToLong(Long::longValue).sum(),
          data);

      // Serialize to JSON
      String jsonContent = objectMapper.writeValueAsString(archiveRecord);

      // Upload to S3
      var putRequest = PutObjectRequest.builder()
          .bucket(bucketName)
          .key(s3Key)
          .contentType("application/json")
          .build();

      s3Client.putObject(putRequest, RequestBody.fromString(jsonContent));

      log.info("Archived analytics for {} to S3: s3://{}/{} ({} media items, {} total views)",
          yearMonth, bucketName, s3Key,
          data.size(), archiveRecord.totalViews());

    } catch (Exception e) {
      log.error("Failed to archive analytics for {} to S3: {}", yearMonth, e.getMessage(), e);
      throw new RuntimeException("S3 archival failed", e);
    }
  }

  /**
   * Archive record for S3 storage.
   */
  public record AnalyticsArchive(
      String period,
      String archivedAt,
      int mediaCount,
      long totalViews,
      Map<String, Long> viewsByMedia) {
  }

  private void executeBatchWrite(List<WriteRequest> writeRequests) {
    if (writeRequests.isEmpty()) {
      return;
    }
    try {
      var request = BatchWriteItemRequest.builder()
          .requestItems(Map.of(TABLE_NAME, new ArrayList<>(writeRequests)))
          .build();
      var response = dynamoDbClient.batchWriteItem(request);
      // Handle unprocessed items (retry logic)
      var unprocessed = response.unprocessedItems();
      int retries = 0;
      while (!unprocessed.isEmpty() && retries < 3) {
        Thread.sleep(100 * (retries + 1)); // Exponential backoff
        var retryRequest = BatchWriteItemRequest.builder()
            .requestItems(unprocessed)
            .build();
        response = dynamoDbClient.batchWriteItem(retryRequest);
        unprocessed = response.unprocessedItems();
        retries++;
      }
      if (!unprocessed.isEmpty()) {
        log.warn("Failed to write {} items after retries", unprocessed.size());
      }
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new RuntimeException("Batch write interrupted", e);
    }
  }
}
