package com.mediaservice.analytics.infrastructure;

import com.mediaservice.shared.config.properties.AnalyticsProperties;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;

import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.util.*;

/**
 * Repository for analytics data in DynamoDB.
 *
 * <p>DynamoDB Schema for Analytics:
 * <ul>
 *   <li>PK: {@code ANALYTICS#TENANT#{tenantId}#VIEWS#{period}#{date}}</li>
 *   <li>SK: {@code {mediaId}}</li>
 *   <li>viewCount: Number of views</li>
 *   <li>snapshotAt: Timestamp of snapshot</li>
 *   <li>ttl: Expiration timestamp (optional)</li>
 * </ul>
 */
@Repository
@Slf4j
public class AnalyticsDynamoDbRepository {

    private static final String TABLE_NAME = "media";
    private static final String ANALYTICS_PK_PREFIX = "ANALYTICS#TENANT#";
    private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;
    private static final int BATCH_WRITE_LIMIT = 25;
    private static final int MAX_RETRIES = 3;

    private final DynamoDbClient dynamoDbClient;
    private final AnalyticsProperties analyticsProperties;

    public AnalyticsDynamoDbRepository(DynamoDbClient dynamoDbClient, AnalyticsProperties analyticsProperties) {
        this.dynamoDbClient = dynamoDbClient;
        this.analyticsProperties = analyticsProperties;
    }

    /**
     * Save daily analytics snapshot to DynamoDB.
     *
     * @param date       the date of the snapshot
     * @param viewCounts map of mediaId to view count
     */
    public void saveDailySnapshot(String tenantId, LocalDate date, Map<String, Long> viewCounts) {
        if (viewCounts.isEmpty()) {
            log.debug("No analytics data to save for {}", date);
            return;
        }

        String dynamoPk = buildPk(tenantId, "DAILY", date.format(DATE_FORMATTER));
        Long ttlEpoch = calculateTtl(date, analyticsProperties.getPersistence().getDailyRetentionDays());

        batchWriteAnalytics(dynamoPk, viewCounts, ttlEpoch);
        log.info("Saved {} daily analytics entries for tenant {} on {}", viewCounts.size(), tenantId, date);
    }

    /**
     * Save monthly analytics snapshot to DynamoDB.
     *
     * @param yearMonth  the year-month (e.g., "2024-01")
     * @param viewCounts map of mediaId to view count
     */
    public void saveMonthlySnapshot(String tenantId, String yearMonth, Map<String, Long> viewCounts) {
        if (viewCounts.isEmpty()) {
            log.debug("No monthly analytics data to save for {}", yearMonth);
            return;
        }

        String dynamoPk = buildPk(tenantId, "MONTHLY", yearMonth);
        LocalDate monthStart = LocalDate.parse(yearMonth + "-01");
        Long ttlEpoch = calculateTtl(monthStart, analyticsProperties.getPersistence().getMonthlyRetentionDays());

        batchWriteAnalytics(dynamoPk, viewCounts, ttlEpoch);
        log.info("Saved {} monthly analytics entries for tenant {} on {}", viewCounts.size(), tenantId, yearMonth);
    }

    /**
     * Query analytics for a specific period and date.
     *
     * @param period  the period type (DAILY, MONTHLY)
     * @param dateKey the date key
     * @return map of mediaId to view count
     */
    public Map<String, Long> queryAnalytics(String tenantId, String period, String dateKey) {
        String dynamoPk = buildPk(tenantId, period, dateKey);
        var results = new LinkedHashMap<String, Long>();

        try {
            var response = dynamoDbClient.query(QueryRequest.builder()
                    .tableName(TABLE_NAME)
                    .keyConditionExpression("PK = :pk")
                    .expressionAttributeValues(Map.of(":pk", AttributeValue.builder().s(dynamoPk).build()))
                    .build());

            for (var item : response.items()) {
                String mediaId = item.get("SK").s();
                long viewCount = Long.parseLong(item.get("viewCount").n());
                results.put(mediaId, viewCount);
            }
        } catch (Exception e) {
            log.error("Failed to query analytics for {}/{}: {}", period, dateKey, e.getMessage());
        }

        return results;
    }

    /**
     * Aggregate daily analytics over a date range.
     *
     * @param startDate start date (inclusive)
     * @param endDate   end date (inclusive)
     * @param limit     maximum results
     * @return map of mediaId to aggregated view count, sorted descending
     */
    public Map<String, Long> aggregateDailyRange(String tenantId, LocalDate startDate, LocalDate endDate, int limit) {
        var aggregated = new HashMap<String, Long>();

        try {
            for (var date = startDate; !date.isAfter(endDate); date = date.plusDays(1)) {
                var dailyData = queryAnalytics(tenantId, "DAILY", date.format(DATE_FORMATTER));
                for (var entry : dailyData.entrySet()) {
                    aggregated.merge(entry.getKey(), entry.getValue(), Long::sum);
                }
            }

            return sortAndLimit(aggregated, limit);
        } catch (Exception e) {
            log.error("Failed to aggregate daily analytics from {} to {}: {}", startDate, endDate, e.getMessage());
            return Collections.emptyMap();
        }
    }

    /**
     * Aggregate monthly analytics for a year-month.
     *
     * @param yearMonth the year-month to aggregate
     * @return map of mediaId to aggregated view count
     */
    public Map<String, Long> aggregateMonthFromDaily(String tenantId, String yearMonth) {
        var aggregated = new HashMap<String, Long>();
        var monthStart = LocalDate.parse(yearMonth + "-01");
        var monthEnd = monthStart.plusMonths(1).minusDays(1);

        for (var date = monthStart; !date.isAfter(monthEnd); date = date.plusDays(1)) {
            var dailyData = queryAnalytics(tenantId, "DAILY", date.format(DATE_FORMATTER));
            for (var entry : dailyData.entrySet()) {
                aggregated.merge(entry.getKey(), entry.getValue(), Long::sum);
            }
        }

        return aggregated;
    }

    private String buildPk(String tenantId, String period, String dateKey) {
        return ANALYTICS_PK_PREFIX + tenantId + "#VIEWS#" + period + "#" + dateKey;
    }

    private Long calculateTtl(LocalDate baseDate, int retentionDays) {
        if (!analyticsProperties.getPersistence().isTtlEnabled()) {
            return null;
        }
        return baseDate.plusDays(retentionDays).atStartOfDay(ZoneOffset.UTC).toEpochSecond();
    }

    private void batchWriteAnalytics(String pk, Map<String, Long> viewCounts, Long ttlEpoch) {
        var writeRequests = new ArrayList<WriteRequest>();

        for (var entry : viewCounts.entrySet()) {
            var item = new HashMap<String, AttributeValue>();
            item.put("PK", AttributeValue.builder().s(pk).build());
            item.put("SK", AttributeValue.builder().s(entry.getKey()).build());
            item.put("viewCount", AttributeValue.builder().n(String.valueOf(entry.getValue())).build());
            item.put("snapshotAt", AttributeValue.builder().s(Instant.now().toString()).build());

            if (ttlEpoch != null) {
                item.put("ttl", AttributeValue.builder().n(String.valueOf(ttlEpoch)).build());
            }

            writeRequests.add(WriteRequest.builder()
                    .putRequest(PutRequest.builder().item(item).build())
                    .build());

            if (writeRequests.size() >= BATCH_WRITE_LIMIT) {
                executeBatchWrite(writeRequests);
                writeRequests.clear();
            }
        }

        if (!writeRequests.isEmpty()) {
            executeBatchWrite(writeRequests);
        }
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

            var unprocessed = response.unprocessedItems();
            int retries = 0;
            while (!unprocessed.isEmpty() && retries < MAX_RETRIES) {
                Thread.sleep(100L * (retries + 1));
                var retryRequest = BatchWriteItemRequest.builder()
                        .requestItems(unprocessed)
                        .build();
                response = dynamoDbClient.batchWriteItem(retryRequest);
                unprocessed = response.unprocessedItems();
                retries++;
            }

            if (!unprocessed.isEmpty()) {
                log.warn("Failed to write {} items after {} retries", unprocessed.size(), MAX_RETRIES);
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new RuntimeException("Batch write interrupted", e);
        }
    }

    private Map<String, Long> sortAndLimit(Map<String, Long> data, int limit) {
        return data.entrySet().stream()
                .sorted((a, b) -> Long.compare(b.getValue(), a.getValue()))
                .limit(limit)
                .collect(java.util.stream.Collectors.toMap(
                        Map.Entry::getKey,
                        Map.Entry::getValue,
                        (e1, e2) -> e1,
                        LinkedHashMap::new));
    }
}
