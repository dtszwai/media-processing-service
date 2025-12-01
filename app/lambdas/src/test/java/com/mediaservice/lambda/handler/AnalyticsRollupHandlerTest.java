package com.mediaservice.lambda.handler;

import com.amazonaws.services.lambda.runtime.Context;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.model.EventType;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.time.LocalDate;
import java.time.YearMonth;
import java.time.format.DateTimeFormatter;
import java.util.*;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Unit tests for AnalyticsRollupHandler using stub implementations.
 *
 * <p>
 * The handler implements the "Roll-Up Pattern":
 * <ul>
 * <li>API handles daily persistence via write-behind (every 5 min)</li>
 * <li>Lambda handles daily S3 archival, monthly aggregation, and monthly S3
 * archival</li>
 * <li>Weekly/yearly data is calculated at read-time from daily snapshots</li>
 * </ul>
 */
class AnalyticsRollupHandlerTest {
  private StubAnalyticsDynamoDbService dynamoDbService;
  private StubS3Client s3Client;
  private ObjectMapper objectMapper;
  private TestableAnalyticsRollupHandler handler;

  @BeforeEach
  void setUp() {
    dynamoDbService = new StubAnalyticsDynamoDbService();
    s3Client = new StubS3Client();
    objectMapper = new ObjectMapper();
    handler = new TestableAnalyticsRollupHandler(dynamoDbService, s3Client, objectMapper);
  }

  @Nested
  @DisplayName("Event Type Handling")
  class EventTypeHandling {

    @Test
    @DisplayName("should return NO_EVENT_TYPE when type is missing")
    void shouldReturnNoEventTypeWhenMissing() {
      Map<String, Object> event = new HashMap<>();
      String result = handler.handleRequest(event, null);
      assertThat(result).isEqualTo("NO_EVENT_TYPE");
    }

    @Test
    @DisplayName("should return UNKNOWN_EVENT_TYPE for invalid type")
    void shouldReturnUnknownEventTypeForInvalid() {
      Map<String, Object> event = Map.of("type", "invalid.event.type");
      String result = handler.handleRequest(event, null);
      assertThat(result).isEqualTo("UNKNOWN_EVENT_TYPE");
    }

    @Test
    @DisplayName("should return UNHANDLED_EVENT_TYPE for media events")
    void shouldReturnUnhandledForMediaEvents() {
      Map<String, Object> event = Map.of("type", "media.v1.process");
      String result = handler.handleRequest(event, null);
      assertThat(result).isEqualTo("UNHANDLED_EVENT_TYPE");
    }

    @Test
    @DisplayName("should return UNHANDLED_EVENT_TYPE for daily rollup (handled by API)")
    void shouldReturnUnhandledForDailyRollup() {
      Map<String, Object> event = Map.of("type", "analytics.v1.rollup.daily");
      String result = handler.handleRequest(event, null);
      assertThat(result).isEqualTo("UNHANDLED_EVENT_TYPE");
    }

    @Test
    @DisplayName("should return UNHANDLED_EVENT_TYPE for weekly rollup (calculated at read-time)")
    void shouldReturnUnhandledForWeeklyRollup() {
      Map<String, Object> event = Map.of("type", "analytics.v1.rollup.weekly");
      String result = handler.handleRequest(event, null);
      assertThat(result).isEqualTo("UNHANDLED_EVENT_TYPE");
    }

    @Test
    @DisplayName("should return UNHANDLED_EVENT_TYPE for yearly rollup (calculated at read-time)")
    void shouldReturnUnhandledForYearlyRollup() {
      Map<String, Object> event = Map.of("type", "analytics.v1.rollup.yearly");
      String result = handler.handleRequest(event, null);
      assertThat(result).isEqualTo("UNHANDLED_EVENT_TYPE");
    }
  }

  @Nested
  @DisplayName("Monthly Rollup")
  class MonthlyRollup {

    @Test
    @DisplayName("should aggregate daily snapshots into monthly summary")
    void shouldAggregateFromDailySnapshots() {
      dynamoDbService.setLockAcquired(true);
      var aggregatedData = new LinkedHashMap<String, Long>();
      aggregatedData.put("media-1", 2000L);
      aggregatedData.put("media-2", 1500L);
      dynamoDbService.setAggregatedData(aggregatedData);

      Map<String, Object> event = Map.of("type", "analytics.v1.rollup.monthly");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(dynamoDbService.wasAggregationCalled()).isTrue();
      assertThat(dynamoDbService.wasAnalyticsPersisted()).isTrue();
      assertThat(dynamoDbService.getLastPersistedPeriod()).isEqualTo("MONTHLY");
    }

    @Test
    @DisplayName("should skip if already processed (idempotency)")
    void shouldSkipIfAlreadyProcessed() {
      dynamoDbService.setLockAcquired(false);

      Map<String, Object> event = Map.of("type", "analytics.v1.rollup.monthly");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(dynamoDbService.wasAnalyticsPersisted()).isFalse();
    }

    @Test
    @DisplayName("should handle empty aggregation gracefully")
    void shouldHandleEmptyAggregation() {
      dynamoDbService.setLockAcquired(true);
      dynamoDbService.setAggregatedData(new LinkedHashMap<>());

      Map<String, Object> event = Map.of("type", "analytics.v1.rollup.monthly");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(dynamoDbService.wasAnalyticsPersisted()).isFalse();
    }
  }

  @Nested
  @DisplayName("Daily Archive")
  class DailyArchive {

    @Test
    @DisplayName("should archive daily data to S3")
    void shouldArchiveDailyDataToS3() {
      dynamoDbService.setLockAcquired(true);
      var dailyData = new LinkedHashMap<String, Long>();
      dailyData.put("media-1", 100L);
      dailyData.put("media-2", 50L);
      dynamoDbService.setQueryResult(dailyData);

      Map<String, Object> event = Map.of("type", "analytics.v1.archive.daily");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(s3Client.wasPutObjectCalled()).isTrue();
      assertThat(s3Client.getLastPutKey()).contains("analytics/daily/");
    }

    @Test
    @DisplayName("should skip archive if no data")
    void shouldSkipDailyArchiveIfNoData() {
      dynamoDbService.setLockAcquired(true);
      dynamoDbService.setQueryResult(new LinkedHashMap<>());

      Map<String, Object> event = Map.of("type", "analytics.v1.archive.daily");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(s3Client.wasPutObjectCalled()).isFalse();
    }

    @Test
    @DisplayName("should skip archive if already processed (idempotency)")
    void shouldSkipDailyArchiveIfAlreadyProcessed() {
      dynamoDbService.setLockAcquired(false);

      Map<String, Object> event = Map.of("type", "analytics.v1.archive.daily");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(s3Client.wasPutObjectCalled()).isFalse();
    }
  }

  @Nested
  @DisplayName("Monthly Archive")
  class MonthlyArchive {

    @Test
    @DisplayName("should archive monthly data to S3")
    void shouldArchiveToS3() {
      dynamoDbService.setLockAcquired(true);
      var monthlyData = new LinkedHashMap<String, Long>();
      monthlyData.put("media-1", 5000L);
      dynamoDbService.setQueryResult(monthlyData);

      Map<String, Object> event = Map.of("type", "analytics.v1.archive.monthly");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(s3Client.wasPutObjectCalled()).isTrue();
      assertThat(s3Client.getLastPutKey()).contains("analytics/");
    }

    @Test
    @DisplayName("should skip archive if no data")
    void shouldSkipIfNoData() {
      dynamoDbService.setLockAcquired(true);
      dynamoDbService.setQueryResult(new LinkedHashMap<>());

      Map<String, Object> event = Map.of("type", "analytics.v1.archive.monthly");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(s3Client.wasPutObjectCalled()).isFalse();
    }

    @Test
    @DisplayName("should skip archive if already processed")
    void shouldSkipArchiveIfAlreadyProcessed() {
      dynamoDbService.setLockAcquired(false);

      Map<String, Object> event = Map.of("type", "analytics.v1.archive.monthly");
      String result = handler.handleRequest(event, null);

      assertThat(result).isEqualTo("OK");
      assertThat(s3Client.wasPutObjectCalled()).isFalse();
    }
  }

  // ========== Stub Classes ==========

  static class StubAnalyticsDynamoDbService {
    private boolean lockAcquired = true;
    private boolean lockWasAcquired = false;
    private boolean analyticsPersisted = false;
    private boolean aggregationCalled = false;
    private String lastPersistedPeriod;
    private Map<String, Long> aggregatedData = new LinkedHashMap<>();
    private Map<String, Long> queryResult = new LinkedHashMap<>();

    void setLockAcquired(boolean acquired) {
      this.lockAcquired = acquired;
    }

    void setAggregatedData(Map<String, Long> data) {
      this.aggregatedData = data;
    }

    void setQueryResult(Map<String, Long> data) {
      this.queryResult = data;
    }

    boolean wasLockAcquired() {
      return lockWasAcquired;
    }

    boolean wasAnalyticsPersisted() {
      return analyticsPersisted;
    }

    boolean wasAggregationCalled() {
      return aggregationCalled;
    }

    String getLastPersistedPeriod() {
      return lastPersistedPeriod;
    }

    boolean acquireIdempotencyLock(String rollupKey) {
      lockWasAcquired = lockAcquired;
      return lockAcquired;
    }

    void persistAnalytics(String period, String periodKey, Map<String, Long> data) {
      if (data != null && !data.isEmpty()) {
        analyticsPersisted = true;
        lastPersistedPeriod = period;
      }
    }

    Map<String, Long> queryAnalytics(String period, String periodKey) {
      return queryResult;
    }

    Map<String, Long> aggregateAnalytics(String period, List<String> periodKeys) {
      aggregationCalled = true;
      return aggregatedData;
    }
  }

  static class StubS3Client {
    private boolean putObjectCalled = false;
    private String lastPutKey;

    boolean wasPutObjectCalled() {
      return putObjectCalled;
    }

    String getLastPutKey() {
      return lastPutKey;
    }

    void putObject(String bucket, String key, String content) {
      putObjectCalled = true;
      lastPutKey = key;
    }
  }

  /**
   * Testable version of AnalyticsRollupHandler without OpenTelemetry
   * dependencies.
   * Mirrors the production handler's logic for daily/monthly archive and monthly
   * rollup.
   */
  static class TestableAnalyticsRollupHandler {
    private final StubAnalyticsDynamoDbService dynamoDbService;
    private final StubS3Client s3Client;
    private final ObjectMapper objectMapper;

    private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;
    private static final DateTimeFormatter MONTH_FORMATTER = DateTimeFormatter.ofPattern("yyyy-MM");

    TestableAnalyticsRollupHandler(StubAnalyticsDynamoDbService dynamoDbService,
        StubS3Client s3Client, ObjectMapper objectMapper) {
      this.dynamoDbService = dynamoDbService;
      this.s3Client = s3Client;
      this.objectMapper = objectMapper;
    }

    public String handleRequest(Map<String, Object> event, Context context) {
      String eventTypeStr = (String) event.get("type");
      if (eventTypeStr == null) {
        return "NO_EVENT_TYPE";
      }

      var eventType = EventType.fromString(eventTypeStr);
      if (eventType == null) {
        return "UNKNOWN_EVENT_TYPE";
      }

      switch (eventType) {
        case DAILY_ARCHIVE -> handleDailyArchive();
        case MONTHLY_ROLLUP -> handleMonthlyRollup();
        case MONTHLY_ARCHIVE -> handleMonthlyArchive();
        default -> {
          // Daily/weekly/yearly rollups are handled by API write-behind or calculated
          // at read-time
          return "UNHANDLED_EVENT_TYPE";
        }
      }

      return "OK";
    }

    private void handleDailyArchive() {
      var yesterday = LocalDate.now().minusDays(1);
      String dateKey = yesterday.format(DATE_FORMATTER);
      String idempotencyKey = "daily-archive-" + dateKey;

      if (!dynamoDbService.acquireIdempotencyLock(idempotencyKey)) {
        return;
      }

      var data = dynamoDbService.queryAnalytics("DAILY", dateKey);

      if (data.isEmpty()) {
        return;
      }

      archiveDailyToS3(dateKey, data);
    }

    private void handleMonthlyRollup() {
      var lastMonth = YearMonth.now().minusMonths(1);
      String monthKey = lastMonth.format(MONTH_FORMATTER);
      String idempotencyKey = "monthly-" + monthKey;

      if (!dynamoDbService.acquireIdempotencyLock(idempotencyKey)) {
        return;
      }

      // Aggregate from DynamoDB daily snapshots (populated by API write-behind)
      var dailyKeys = getDailyKeysForMonth(lastMonth);
      var data = dynamoDbService.aggregateAnalytics("DAILY", dailyKeys);

      if (data.isEmpty()) {
        return;
      }

      dynamoDbService.persistAnalytics("MONTHLY", monthKey, data);
    }

    private void handleMonthlyArchive() {
      var lastMonth = YearMonth.now().minusMonths(1);
      String monthKey = lastMonth.format(MONTH_FORMATTER);
      String idempotencyKey = "archive-" + monthKey;

      if (!dynamoDbService.acquireIdempotencyLock(idempotencyKey)) {
        return;
      }

      var data = dynamoDbService.queryAnalytics("MONTHLY", monthKey);

      if (data.isEmpty()) {
        return;
      }

      archiveMonthlyToS3(monthKey, data);
    }

    private void archiveDailyToS3(String dateKey, Map<String, Long> data) {
      try {
        String[] parts = dateKey.split("-");
        String year = parts[0];
        String month = parts[1];
        String s3Key = String.format("analytics/daily/%s/%s/analytics-%s.json", year, month, dateKey);

        var archive = Map.of(
            "period", dateKey,
            "archivedAt", Instant.now().toString(),
            "mediaCount", data.size(),
            "totalViews", data.values().stream().mapToLong(Long::longValue).sum(),
            "viewsByMedia", data);

        String jsonContent = objectMapper.writeValueAsString(archive);
        s3Client.putObject("test-bucket", s3Key, jsonContent);
      } catch (Exception e) {
        throw new RuntimeException("S3 daily archival failed", e);
      }
    }

    private void archiveMonthlyToS3(String monthKey, Map<String, Long> data) {
      try {
        String[] parts = monthKey.split("-");
        String year = parts[0];
        String month = parts[1];
        String s3Key = String.format("analytics/monthly/%s/%s/analytics-%s.json", year, month, monthKey);

        var archive = Map.of(
            "period", monthKey,
            "archivedAt", Instant.now().toString(),
            "mediaCount", data.size(),
            "totalViews", data.values().stream().mapToLong(Long::longValue).sum(),
            "viewsByMedia", data);

        String jsonContent = objectMapper.writeValueAsString(archive);
        s3Client.putObject("test-bucket", s3Key, jsonContent);
      } catch (Exception e) {
        throw new RuntimeException("S3 monthly archival failed", e);
      }
    }

    private List<String> getDailyKeysForMonth(YearMonth yearMonth) {
      var keys = new ArrayList<String>();
      var start = yearMonth.atDay(1);
      var end = yearMonth.atEndOfMonth();
      for (var date = start; !date.isAfter(end); date = date.plusDays(1)) {
        keys.add(date.format(DATE_FORMATTER));
      }
      return keys;
    }
  }
}
