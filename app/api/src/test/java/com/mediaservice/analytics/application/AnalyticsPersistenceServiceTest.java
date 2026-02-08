package com.mediaservice.analytics.application;

import com.mediaservice.analytics.infrastructure.AnalyticsDynamoDbRepository;
import com.mediaservice.analytics.infrastructure.AnalyticsS3ArchiveService;
import com.mediaservice.shared.config.properties.AnalyticsProperties;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.SetOperations;
import org.springframework.data.redis.core.ZSetOperations;
import org.springframework.data.redis.core.ZSetOperations.TypedTuple;

import java.time.LocalDate;
import java.util.*;
import java.util.LinkedHashMap;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class AnalyticsPersistenceServiceTest {
  private static final String TENANT_ID = "tenant-1";

  @Mock
  private AnalyticsDynamoDbRepository dynamoDbRepository;

  @Mock
  private AnalyticsS3ArchiveService s3ArchiveService;

  @Mock
  private StringRedisTemplate redisTemplate;

  @Mock
  private SetOperations<String, String> setOperations;

  @Mock
  private ZSetOperations<String, String> zSetOperations;

  @Mock
  private AnalyticsProperties analyticsProperties;

  @Mock
  private AnalyticsProperties.PersistenceConfig persistenceConfig;

  private AnalyticsPersistenceService service;

  @BeforeEach
  void setUp() {
    service = new AnalyticsPersistenceService(
        dynamoDbRepository, s3ArchiveService, redisTemplate, analyticsProperties);
    lenient().when(analyticsProperties.getPersistence()).thenReturn(persistenceConfig);
  }

  @Nested
  @DisplayName("snapshotDailyAnalytics")
  class SnapshotDailyAnalytics {

    @Test
    @DisplayName("should skip if analytics disabled")
    void shouldSkipIfAnalyticsDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(false);

      service.snapshotDailyAnalytics();

      verifyNoInteractions(dynamoDbRepository);
      verifyNoInteractions(redisTemplate);
    }

    @Test
    @DisplayName("should skip if persistence disabled")
    void shouldSkipIfPersistenceDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(true);
      when(persistenceConfig.isEnabled()).thenReturn(false);

      service.snapshotDailyAnalytics();

      verifyNoInteractions(dynamoDbRepository);
      verifyNoInteractions(redisTemplate);
    }

    @Test
    @DisplayName("should snapshot yesterday's data when enabled")
    void shouldSnapshotYesterdaysData() {
      when(analyticsProperties.isEnabled()).thenReturn(true);
      when(persistenceConfig.isEnabled()).thenReturn(true);
      when(redisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.members("analytics:tenants")).thenReturn(Set.of(TENANT_ID));
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      // Return empty set so it exits early without needing retention days
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(Set.of());

      service.snapshotDailyAnalytics();

      verify(redisTemplate).opsForZSet();
    }
  }

  @Nested
  @DisplayName("snapshotDayToDb")
  class SnapshotDayToDb {

    @Test
    @DisplayName("should skip if no data in Redis")
    void shouldSkipIfNoData() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(null);

      service.snapshotDayToDb(TENANT_ID, date);

      verifyNoInteractions(dynamoDbRepository);
    }

    @Test
    @DisplayName("should skip if empty data in Redis")
    void shouldSkipIfEmptyData() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(Set.of());

      service.snapshotDayToDb(TENANT_ID, date);

      verifyNoInteractions(dynamoDbRepository);
    }

    @Test
    @DisplayName("should persist data to DynamoDB when data exists")
    void shouldPersistDataToDynamoDb() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);

      Set<TypedTuple<String>> entries = Set.of(
          createTypedTuple("media-1", 100.0),
          createTypedTuple("media-2", 50.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      service.snapshotDayToDb(TENANT_ID, date);

      verify(dynamoDbRepository).saveDailySnapshot(eq(TENANT_ID), eq(date), argThat(map ->
          map.size() == 2 &&
          map.get("media-1") == 100L &&
          map.get("media-2") == 50L));
    }

    @Test
    @DisplayName("should skip entries with null mediaId")
    void shouldSkipEntriesWithNullMediaId() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);

      Set<TypedTuple<String>> entries = Set.of(
          createTypedTuple(null, 100.0),
          createTypedTuple("media-1", 50.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      service.snapshotDayToDb(TENANT_ID, date);

      verify(dynamoDbRepository).saveDailySnapshot(eq(TENANT_ID), eq(date), argThat(map ->
          map.size() == 1 && map.containsKey("media-1")));
    }

    @Test
    @DisplayName("should skip entries with zero view count")
    void shouldSkipEntriesWithZeroViewCount() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);

      Set<TypedTuple<String>> entries = Set.of(
          createTypedTuple("media-1", 0.0),
          createTypedTuple("media-2", 50.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      service.snapshotDayToDb(TENANT_ID, date);

      verify(dynamoDbRepository).saveDailySnapshot(eq(TENANT_ID), eq(date), argThat(map ->
          map.size() == 1 && map.containsKey("media-2")));
    }
  }

  @Nested
  @DisplayName("snapshotMonthToDb")
  class SnapshotMonthToDb {

    @Test
    @DisplayName("should aggregate daily snapshots into monthly")
    void shouldAggregateDailySnapshots() {
      String yearMonth = "2024-01";

      Map<String, Long> aggregatedData = Map.of("media-1", 10L, "media-2", 5L);
      when(dynamoDbRepository.aggregateMonthFromDaily(TENANT_ID, yearMonth)).thenReturn(aggregatedData);

      var result = service.snapshotMonthToDb(TENANT_ID, yearMonth);

      verify(dynamoDbRepository).aggregateMonthFromDaily(TENANT_ID, yearMonth);
      verify(dynamoDbRepository).saveMonthlySnapshot(TENANT_ID, yearMonth, aggregatedData);
      assertThat(result).isNotEmpty();
      assertThat(result).containsEntry("media-1", 10L);
      assertThat(result).containsEntry("media-2", 5L);
    }

    @Test
    @DisplayName("should skip save if no data found for month")
    void shouldSkipIfNoDataForMonth() {
      String yearMonth = "2024-01";

      when(dynamoDbRepository.aggregateMonthFromDaily(TENANT_ID, yearMonth)).thenReturn(Map.of());

      var result = service.snapshotMonthToDb(TENANT_ID, yearMonth);

      verify(dynamoDbRepository).aggregateMonthFromDaily(TENANT_ID, yearMonth);
      verify(dynamoDbRepository, never()).saveMonthlySnapshot(anyString(), anyString(), any());
      assertThat(result).isEmpty();
    }
  }

  @Nested
  @DisplayName("restoreFromDb")
  class RestoreFromDb {

    @Test
    @DisplayName("should restore data from DynamoDB to Redis")
    void shouldRestoreDataToRedis() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.getDailyRetentionDays()).thenReturn(90);

      Map<String, Long> viewCounts = Map.of("media-1", 100L, "media-2", 50L);
      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "DAILY", "2024-01-15")).thenReturn(viewCounts);

      service.restoreFromDb(TENANT_ID, date);

      verify(zSetOperations).add(contains("views:daily:" + TENANT_ID + ":2024-01-15"), eq("media-1"), eq(100.0));
      verify(zSetOperations).add(contains("views:daily:" + TENANT_ID + ":2024-01-15"), eq("media-2"), eq(50.0));
      verify(zSetOperations).incrementScore(eq("views:total:" + TENANT_ID), eq("media-1"), eq(100.0));
      verify(zSetOperations).incrementScore(eq("views:total:" + TENANT_ID), eq("media-2"), eq(50.0));
    }

    @Test
    @DisplayName("should skip if no data in DynamoDB")
    void shouldSkipIfNoDataInDynamoDb() {
      LocalDate date = LocalDate.of(2024, 1, 15);

      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "DAILY", "2024-01-15")).thenReturn(Map.of());

      service.restoreFromDb(TENANT_ID, date);

      verifyNoInteractions(redisTemplate);
    }

    @Test
    @DisplayName("should set TTL on restored Redis key")
    void shouldSetTtlOnRestoredKey() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.getDailyRetentionDays()).thenReturn(90);

      Map<String, Long> viewCounts = Map.of("media-1", 100L);
      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "DAILY", "2024-01-15")).thenReturn(viewCounts);

      service.restoreFromDb(TENANT_ID, date);

      verify(redisTemplate).expire(contains("views:daily:" + TENANT_ID + ":2024-01-15"), eq(90L), any());
    }
  }

  @Nested
  @DisplayName("getHistoricalAnalytics")
  class GetHistoricalAnalytics {

    @Test
    @DisplayName("should return historical data sorted by view count")
    void shouldReturnSortedHistoricalData() {
      // Note: The service sorts the data from the repository
      Map<String, Long> unsortedData = new LinkedHashMap<>();
      unsortedData.put("media-1", 50L);
      unsortedData.put("media-2", 100L);
      unsortedData.put("media-3", 75L);
      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "DAILY", "2024-01-15")).thenReturn(unsortedData);

      var result = service.getHistoricalAnalytics(TENANT_ID, "DAILY", "2024-01-15", 10);

      assertThat(result).hasSize(3);
      // Should be sorted descending: media-2 (100), media-3 (75), media-1 (50)
      var keys = new ArrayList<>(result.keySet());
      assertThat(keys).containsExactly("media-2", "media-3", "media-1");
      assertThat(result.get("media-2")).isEqualTo(100L);
      assertThat(result.get("media-3")).isEqualTo(75L);
      assertThat(result.get("media-1")).isEqualTo(50L);
    }

    @Test
    @DisplayName("should respect limit parameter")
    void shouldRespectLimitParameter() {
      Map<String, Long> unsortedData = new LinkedHashMap<>();
      unsortedData.put("media-1", 50L);
      unsortedData.put("media-2", 100L);
      unsortedData.put("media-3", 75L);
      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "DAILY", "2024-01-15")).thenReturn(unsortedData);

      var result = service.getHistoricalAnalytics(TENANT_ID, "DAILY", "2024-01-15", 2);

      assertThat(result).hasSize(2);
      var keys = new ArrayList<>(result.keySet());
      assertThat(keys).containsExactly("media-2", "media-3");
    }

    @Test
    @DisplayName("should return empty map when no data")
    void shouldReturnEmptyMapWhenNoData() {
      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "DAILY", "2024-01-15")).thenReturn(Map.of());

      var result = service.getHistoricalAnalytics(TENANT_ID, "DAILY", "2024-01-15", 10);

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should query correct period and date key")
    void shouldQueryCorrectPeriodAndDateKey() {
      when(dynamoDbRepository.queryAnalytics(TENANT_ID, "MONTHLY", "2024-01")).thenReturn(Map.of());

      service.getHistoricalAnalytics(TENANT_ID, "MONTHLY", "2024-01", 10);

      verify(dynamoDbRepository).queryAnalytics(TENANT_ID, "MONTHLY", "2024-01");
    }
  }

  @Nested
  @DisplayName("archiveToS3")
  class ArchiveToS3 {

    @Test
    @DisplayName("should delegate to archive service")
    void shouldDelegateToArchiveService() {
      Map<String, Long> data = Map.of("media-1", 100L, "media-2", 50L);

      service.archiveToS3(TENANT_ID, "2024-01", data);

      verify(s3ArchiveService).archive(TENANT_ID, "2024-01", data);
    }
  }

  @Nested
  @DisplayName("archiveMonthlyAnalyticsToS3")
  class ArchiveMonthlyAnalyticsToS3 {

    @Test
    @DisplayName("should skip if S3 archival disabled")
    void shouldSkipIfS3ArchivalDisabled() {
      when(s3ArchiveService.isEnabled()).thenReturn(false);

      service.archiveMonthlyAnalyticsToS3();

      verifyNoInteractions(dynamoDbRepository);
      verify(s3ArchiveService, never()).archive(anyString(), anyString(), any());
    }

    @Test
    @DisplayName("should archive when enabled and data exists")
    void shouldArchiveWhenEnabledAndDataExists() {
      when(s3ArchiveService.isEnabled()).thenReturn(true);
      Map<String, Long> monthlyData = Map.of("media-1", 100L);
      when(redisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.members("analytics:tenants")).thenReturn(Set.of(TENANT_ID));
      when(dynamoDbRepository.aggregateMonthFromDaily(eq(TENANT_ID), anyString())).thenReturn(monthlyData);

      service.archiveMonthlyAnalyticsToS3();

      verify(dynamoDbRepository).aggregateMonthFromDaily(eq(TENANT_ID), anyString());
      verify(dynamoDbRepository).saveMonthlySnapshot(eq(TENANT_ID), anyString(), eq(monthlyData));
      verify(s3ArchiveService).archive(eq(TENANT_ID), anyString(), eq(monthlyData));
    }
  }

  private TypedTuple<String> createTypedTuple(String value, Double score) {
    return new TypedTuple<>() {
      @Override
      public String getValue() {
        return value;
      }

      @Override
      public Double getScore() {
        return score;
      }

      @Override
      public int compareTo(TypedTuple<String> o) {
        return Double.compare(score, o.getScore());
      }
    };
  }

  private static String contains(String substring) {
    return argThat(arg -> arg != null && arg.contains(substring));
  }
}
