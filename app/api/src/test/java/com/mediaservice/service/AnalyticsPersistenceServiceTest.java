package com.mediaservice.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.config.AnalyticsProperties;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.ZSetOperations;
import org.springframework.data.redis.core.ZSetOperations.TypedTuple;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;
import software.amazon.awssdk.services.s3.model.PutObjectResponse;

import java.time.LocalDate;
import java.util.*;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class AnalyticsPersistenceServiceTest {

  @Mock
  private DynamoDbClient dynamoDbClient;

  @Mock
  private S3Client s3Client;

  @Mock
  private StringRedisTemplate redisTemplate;

  @Mock
  private ZSetOperations<String, String> zSetOperations;

  @Mock
  private AnalyticsProperties analyticsProperties;

  @Mock
  private AnalyticsProperties.PersistenceConfig persistenceConfig;

  @Mock
  private AnalyticsProperties.S3ArchiveConfig s3ArchiveConfig;

  private ObjectMapper objectMapper;
  private AnalyticsPersistenceService service;

  private static final String TEST_BUCKET = "test-bucket";

  @BeforeEach
  void setUp() {
    objectMapper = new ObjectMapper();
    service = new AnalyticsPersistenceService(
        dynamoDbClient, s3Client, redisTemplate,
        analyticsProperties, objectMapper, TEST_BUCKET);
    lenient().when(analyticsProperties.getPersistence()).thenReturn(persistenceConfig);
    lenient().when(persistenceConfig.getS3Archive()).thenReturn(s3ArchiveConfig);
  }

  @Nested
  @DisplayName("snapshotDailyAnalytics")
  class SnapshotDailyAnalytics {

    @Test
    @DisplayName("should skip if analytics disabled")
    void shouldSkipIfAnalyticsDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(false);

      service.snapshotDailyAnalytics();

      verifyNoInteractions(dynamoDbClient);
      verifyNoInteractions(redisTemplate);
    }

    @Test
    @DisplayName("should skip if persistence disabled")
    void shouldSkipIfPersistenceDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(true);
      when(persistenceConfig.isEnabled()).thenReturn(false);

      service.snapshotDailyAnalytics();

      verifyNoInteractions(dynamoDbClient);
      verifyNoInteractions(redisTemplate);
    }

    @Test
    @DisplayName("should snapshot yesterday's data when enabled")
    void shouldSnapshotYesterdaysData() {
      when(analyticsProperties.isEnabled()).thenReturn(true);
      when(persistenceConfig.isEnabled()).thenReturn(true);
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

      service.snapshotDayToDb(date);

      verifyNoInteractions(dynamoDbClient);
    }

    @Test
    @DisplayName("should skip if empty data in Redis")
    void shouldSkipIfEmptyData() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(Set.of());

      service.snapshotDayToDb(date);

      verifyNoInteractions(dynamoDbClient);
    }

    @Test
    @DisplayName("should persist data to DynamoDB without TTL when disabled")
    void shouldPersistDataToDynamoDbWithoutTtl() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      Set<TypedTuple<String>> entries = Set.of(
          createTypedTuple("media-1", 100.0),
          createTypedTuple("media-2", 50.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      var batchResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class))).thenReturn(batchResponse);

      service.snapshotDayToDb(date);

      ArgumentCaptor<BatchWriteItemRequest> captor = ArgumentCaptor.forClass(BatchWriteItemRequest.class);
      verify(dynamoDbClient).batchWriteItem(captor.capture());

      var request = captor.getValue();
      var writeRequests = request.requestItems().get("media");
      assertThat(writeRequests).hasSize(2);

      // Verify no TTL attribute when disabled
      var item = writeRequests.get(0).putRequest().item();
      assertThat(item).doesNotContainKey("ttl");
    }

    @Test
    @DisplayName("should persist data to DynamoDB with TTL when enabled")
    void shouldPersistDataToDynamoDbWithTtl() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(true);
      when(persistenceConfig.getDailyRetentionDays()).thenReturn(90);

      Set<TypedTuple<String>> entries = Set.of(createTypedTuple("media-1", 100.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      var batchResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class))).thenReturn(batchResponse);

      service.snapshotDayToDb(date);

      ArgumentCaptor<BatchWriteItemRequest> captor = ArgumentCaptor.forClass(BatchWriteItemRequest.class);
      verify(dynamoDbClient).batchWriteItem(captor.capture());

      var request = captor.getValue();
      var writeRequests = request.requestItems().get("media");
      assertThat(writeRequests).hasSize(1);

      // Verify TTL attribute is present when enabled
      var item = writeRequests.get(0).putRequest().item();
      assertThat(item).containsKey("ttl");
    }

    @Test
    @DisplayName("should batch writes when more than 25 items")
    void shouldBatchWritesWhenMoreThan25Items() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      // Create 30 entries
      Set<TypedTuple<String>> entries = new HashSet<>();
      for (int i = 0; i < 30; i++) {
        entries.add(createTypedTuple("media-" + i, (double) (100 - i)));
      }
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      var batchResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class))).thenReturn(batchResponse);

      service.snapshotDayToDb(date);

      // Should be called twice: once with 25 items, once with 5 items
      verify(dynamoDbClient, times(2)).batchWriteItem(any(BatchWriteItemRequest.class));
    }

    @Test
    @DisplayName("should skip entries with null mediaId")
    void shouldSkipEntriesWithNullMediaId() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      Set<TypedTuple<String>> entries = Set.of(
          createTypedTuple(null, 100.0),
          createTypedTuple("media-1", 50.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      var batchResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class))).thenReturn(batchResponse);

      service.snapshotDayToDb(date);

      ArgumentCaptor<BatchWriteItemRequest> captor = ArgumentCaptor.forClass(BatchWriteItemRequest.class);
      verify(dynamoDbClient).batchWriteItem(captor.capture());

      var request = captor.getValue();
      var writeRequests = request.requestItems().get("media");
      assertThat(writeRequests).hasSize(1);
    }

    @Test
    @DisplayName("should skip entries with zero view count")
    void shouldSkipEntriesWithZeroViewCount() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      Set<TypedTuple<String>> entries = Set.of(
          createTypedTuple("media-1", 0.0),
          createTypedTuple("media-2", 50.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      var batchResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class))).thenReturn(batchResponse);

      service.snapshotDayToDb(date);

      ArgumentCaptor<BatchWriteItemRequest> captor = ArgumentCaptor.forClass(BatchWriteItemRequest.class);
      verify(dynamoDbClient).batchWriteItem(captor.capture());

      var request = captor.getValue();
      var writeRequests = request.requestItems().get("media");
      assertThat(writeRequests).hasSize(1);
    }

    @Test
    @DisplayName("should throw exception on DynamoDB error")
    void shouldThrowExceptionOnDynamoDbError() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      Set<TypedTuple<String>> entries = Set.of(createTypedTuple("media-1", 100.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class)))
          .thenThrow(DynamoDbException.builder().message("Connection error").build());

      assertThatThrownBy(() -> service.snapshotDayToDb(date))
          .isInstanceOf(DynamoDbException.class);
    }
  }

  @Nested
  @DisplayName("snapshotMonthToDb")
  class SnapshotMonthToDb {

    @Test
    @DisplayName("should aggregate daily snapshots into monthly")
    void shouldAggregateDailySnapshots() {
      String yearMonth = "2024-01";
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      // Create mock responses for daily queries (31 days in January)
      var emptyResponse = QueryResponse.builder().items(List.of()).build();
      var dayWithDataResponse = QueryResponse.builder()
          .items(List.of(
              Map.of(
                  "SK", AttributeValue.builder().s("media-1").build(),
                  "viewCount", AttributeValue.builder().n("10").build()),
              Map.of(
                  "SK", AttributeValue.builder().s("media-2").build(),
                  "viewCount", AttributeValue.builder().n("5").build())))
          .build();

      // Return data for first day, empty for rest
      when(dynamoDbClient.query(any(QueryRequest.class)))
          .thenReturn(dayWithDataResponse)
          .thenReturn(emptyResponse);

      var batchResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();
      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class))).thenReturn(batchResponse);

      var result = service.snapshotMonthToDb(yearMonth);

      verify(dynamoDbClient, atLeastOnce()).query(any(QueryRequest.class));
      verify(dynamoDbClient).batchWriteItem(any(BatchWriteItemRequest.class));
      assertThat(result).isNotEmpty();
    }

    @Test
    @DisplayName("should skip if no data found for month")
    void shouldSkipIfNoDataForMonth() {
      String yearMonth = "2024-01";

      var emptyResponse = QueryResponse.builder().items(List.of()).build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(emptyResponse);

      var result = service.snapshotMonthToDb(yearMonth);

      verify(dynamoDbClient, atLeastOnce()).query(any(QueryRequest.class));
      verify(dynamoDbClient, never()).batchWriteItem(any(BatchWriteItemRequest.class));
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

      var response = QueryResponse.builder()
          .items(List.of(
              Map.of(
                  "SK", AttributeValue.builder().s("media-1").build(),
                  "viewCount", AttributeValue.builder().n("100").build()),
              Map.of(
                  "SK", AttributeValue.builder().s("media-2").build(),
                  "viewCount", AttributeValue.builder().n("50").build())))
          .build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(response);

      service.restoreFromDb(date);

      verify(zSetOperations).add(contains("views:daily:2024-01-15"), eq("media-1"), eq(100.0));
      verify(zSetOperations).add(contains("views:daily:2024-01-15"), eq("media-2"), eq(50.0));
      verify(zSetOperations).incrementScore(eq("views:total"), eq("media-1"), eq(100.0));
      verify(zSetOperations).incrementScore(eq("views:total"), eq("media-2"), eq(50.0));
    }

    @Test
    @DisplayName("should skip if no data in DynamoDB")
    void shouldSkipIfNoDataInDynamoDb() {
      LocalDate date = LocalDate.of(2024, 1, 15);

      var emptyResponse = QueryResponse.builder().items(List.of()).build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(emptyResponse);

      service.restoreFromDb(date);

      verifyNoInteractions(redisTemplate);
    }

    @Test
    @DisplayName("should set TTL on restored Redis key")
    void shouldSetTtlOnRestoredKey() {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.getDailyRetentionDays()).thenReturn(90);

      var response = QueryResponse.builder()
          .items(List.of(
              Map.of(
                  "SK", AttributeValue.builder().s("media-1").build(),
                  "viewCount", AttributeValue.builder().n("100").build())))
          .build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(response);

      service.restoreFromDb(date);

      verify(redisTemplate).expire(contains("views:daily:2024-01-15"), eq(90L), any());
    }
  }

  @Nested
  @DisplayName("getHistoricalAnalytics")
  class GetHistoricalAnalytics {

    @Test
    @DisplayName("should return historical data sorted by view count")
    void shouldReturnSortedHistoricalData() {
      var response = QueryResponse.builder()
          .items(List.of(
              Map.of(
                  "SK", AttributeValue.builder().s("media-1").build(),
                  "viewCount", AttributeValue.builder().n("50").build()),
              Map.of(
                  "SK", AttributeValue.builder().s("media-2").build(),
                  "viewCount", AttributeValue.builder().n("100").build()),
              Map.of(
                  "SK", AttributeValue.builder().s("media-3").build(),
                  "viewCount", AttributeValue.builder().n("75").build())))
          .build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(response);

      var result = service.getHistoricalAnalytics("DAILY", "2024-01-15", 10);

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
      var response = QueryResponse.builder()
          .items(List.of(
              Map.of(
                  "SK", AttributeValue.builder().s("media-1").build(),
                  "viewCount", AttributeValue.builder().n("50").build()),
              Map.of(
                  "SK", AttributeValue.builder().s("media-2").build(),
                  "viewCount", AttributeValue.builder().n("100").build()),
              Map.of(
                  "SK", AttributeValue.builder().s("media-3").build(),
                  "viewCount", AttributeValue.builder().n("75").build())))
          .build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(response);

      var result = service.getHistoricalAnalytics("DAILY", "2024-01-15", 2);

      assertThat(result).hasSize(2);
      var keys = new ArrayList<>(result.keySet());
      assertThat(keys).containsExactly("media-2", "media-3");
    }

    @Test
    @DisplayName("should return empty map on error")
    void shouldReturnEmptyMapOnError() {
      when(dynamoDbClient.query(any(QueryRequest.class)))
          .thenThrow(DynamoDbException.builder().message("Error").build());

      var result = service.getHistoricalAnalytics("DAILY", "2024-01-15", 10);

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should query correct partition key for MONTHLY period")
    void shouldQueryCorrectPkForMonthlyPeriod() {
      var emptyResponse = QueryResponse.builder().items(List.of()).build();
      when(dynamoDbClient.query(any(QueryRequest.class))).thenReturn(emptyResponse);

      service.getHistoricalAnalytics("MONTHLY", "2024-01", 10);

      ArgumentCaptor<QueryRequest> captor = ArgumentCaptor.forClass(QueryRequest.class);
      verify(dynamoDbClient).query(captor.capture());

      var request = captor.getValue();
      var pk = request.expressionAttributeValues().get(":pk").s();
      assertThat(pk).isEqualTo("ANALYTICS#VIEWS#MONTHLY#2024-01");
    }
  }

  @Nested
  @DisplayName("batch write retries")
  class BatchWriteRetries {

    @Test
    @DisplayName("should retry on unprocessed items")
    void shouldRetryOnUnprocessedItems() throws Exception {
      LocalDate date = LocalDate.of(2024, 1, 15);
      when(redisTemplate.opsForZSet()).thenReturn(zSetOperations);
      when(persistenceConfig.isTtlEnabled()).thenReturn(false);

      Set<TypedTuple<String>> entries = Set.of(createTypedTuple("media-1", 100.0));
      when(zSetOperations.rangeWithScores(anyString(), anyLong(), anyLong())).thenReturn(entries);

      // First call returns unprocessed items, second call succeeds
      var unprocessedItems = Map.of("media", List.of(
          WriteRequest.builder().putRequest(PutRequest.builder()
              .item(Map.of("PK", AttributeValue.builder().s("test").build()))
              .build()).build()));
      var firstResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(unprocessedItems)
          .build();
      var secondResponse = BatchWriteItemResponse.builder()
          .unprocessedItems(Map.of())
          .build();

      when(dynamoDbClient.batchWriteItem(any(BatchWriteItemRequest.class)))
          .thenReturn(firstResponse)
          .thenReturn(secondResponse);

      service.snapshotDayToDb(date);

      // Should call twice: initial + retry
      verify(dynamoDbClient, times(2)).batchWriteItem(any(BatchWriteItemRequest.class));
    }
  }

  @Nested
  @DisplayName("archiveToS3")
  class ArchiveToS3 {

    @Test
    @DisplayName("should archive data to S3")
    void shouldArchiveDataToS3() {
      when(s3ArchiveConfig.isEnabled()).thenReturn(true);
      when(s3ArchiveConfig.getPrefix()).thenReturn("analytics/");
      when(s3Client.putObject(any(PutObjectRequest.class), any(RequestBody.class)))
          .thenReturn(PutObjectResponse.builder().build());

      Map<String, Long> data = Map.of("media-1", 100L, "media-2", 50L);

      service.archiveToS3("2024-01", data);

      ArgumentCaptor<PutObjectRequest> captor = ArgumentCaptor.forClass(PutObjectRequest.class);
      verify(s3Client).putObject(captor.capture(), any(RequestBody.class));

      var request = captor.getValue();
      assertThat(request.bucket()).isEqualTo(TEST_BUCKET);
      assertThat(request.key()).isEqualTo("analytics/2024/01/analytics-2024-01.json");
      assertThat(request.contentType()).isEqualTo("application/json");
    }

    @Test
    @DisplayName("should skip if S3 archival disabled")
    void shouldSkipIfS3ArchivalDisabled() {
      when(s3ArchiveConfig.isEnabled()).thenReturn(false);

      Map<String, Long> data = Map.of("media-1", 100L);

      service.archiveToS3("2024-01", data);

      verifyNoInteractions(s3Client);
    }

    @Test
    @DisplayName("should throw exception on S3 error")
    void shouldThrowExceptionOnS3Error() {
      when(s3ArchiveConfig.isEnabled()).thenReturn(true);
      when(s3ArchiveConfig.getPrefix()).thenReturn("analytics/");
      when(s3Client.putObject(any(PutObjectRequest.class), any(RequestBody.class)))
          .thenThrow(new RuntimeException("S3 error"));

      Map<String, Long> data = Map.of("media-1", 100L);

      assertThatThrownBy(() -> service.archiveToS3("2024-01", data))
          .isInstanceOf(RuntimeException.class)
          .hasMessageContaining("S3 archival failed");
    }
  }

  @Nested
  @DisplayName("archiveMonthlyAnalyticsToS3")
  class ArchiveMonthlyAnalyticsToS3 {

    @Test
    @DisplayName("should skip if analytics disabled")
    void shouldSkipIfAnalyticsDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(false);

      service.archiveMonthlyAnalyticsToS3();

      verifyNoInteractions(dynamoDbClient);
      verifyNoInteractions(s3Client);
    }

    @Test
    @DisplayName("should skip if persistence disabled")
    void shouldSkipIfPersistenceDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(true);
      when(persistenceConfig.isEnabled()).thenReturn(false);

      service.archiveMonthlyAnalyticsToS3();

      verifyNoInteractions(dynamoDbClient);
      verifyNoInteractions(s3Client);
    }

    @Test
    @DisplayName("should skip if S3 archival disabled")
    void shouldSkipIfS3ArchivalDisabled() {
      when(analyticsProperties.isEnabled()).thenReturn(true);
      when(persistenceConfig.isEnabled()).thenReturn(true);
      when(s3ArchiveConfig.isEnabled()).thenReturn(false);

      service.archiveMonthlyAnalyticsToS3();

      verifyNoInteractions(dynamoDbClient);
      verifyNoInteractions(s3Client);
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
