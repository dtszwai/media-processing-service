package com.mediaservice.analytics.application;

import com.mediaservice.analytics.infrastructure.AnalyticsDynamoDbRepository;
import com.mediaservice.analytics.infrastructure.AnalyticsS3ArchiveService;
import com.mediaservice.shared.config.properties.AnalyticsProperties;
import lombok.extern.slf4j.Slf4j;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.time.LocalDate;
import java.time.YearMonth;
import java.time.format.DateTimeFormatter;
import java.util.*;
import java.util.concurrent.TimeUnit;

/**
 * Orchestrator for analytics persistence operations.
 *
 * <p>Coordinates the snapshot strategy:
 * <ul>
 *   <li>Real-time data stays in Redis (fast reads/writes)</li>
 *   <li>Daily snapshots are persisted to DynamoDB via {@link AnalyticsDynamoDbRepository}</li>
 *   <li>Monthly archives are stored in S3 via {@link AnalyticsS3ArchiveService}</li>
 *   <li>On startup, can restore Redis from DynamoDB if needed</li>
 * </ul>
 */
@Service
@Slf4j
public class AnalyticsPersistenceService {

    private final AnalyticsDynamoDbRepository dynamoDbRepository;
    private final AnalyticsS3ArchiveService s3ArchiveService;
    private final StringRedisTemplate redisTemplate;
    private final AnalyticsProperties analyticsProperties;

    private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;

    // Redis key prefixes (must match AnalyticsService)
    private static final String VIEWS_DAILY_PREFIX = "views:daily:";
    private static final String VIEWS_TOTAL_KEY = "views:total";

    // Distributed lock for write-behind
    private static final String WRITE_BEHIND_LOCK_KEY = "analytics:writebehind:lock";
    private static final Duration WRITE_BEHIND_LOCK_TTL = Duration.ofMinutes(2);

    public AnalyticsPersistenceService(
            AnalyticsDynamoDbRepository dynamoDbRepository,
            AnalyticsS3ArchiveService s3ArchiveService,
            StringRedisTemplate redisTemplate,
            AnalyticsProperties analyticsProperties) {
        this.dynamoDbRepository = dynamoDbRepository;
        this.s3ArchiveService = s3ArchiveService;
        this.redisTemplate = redisTemplate;
        this.analyticsProperties = analyticsProperties;
    }

    /**
     * Write-behind task: Persist current day's analytics from Redis to DynamoDB.
     * Runs every 5 minutes to ensure data durability.
     */
    @Scheduled(fixedRate = 300000)
    public void writeBehindPersistence() {
        if (!isEnabled()) {
            return;
        }

        if (!acquireWriteBehindLock()) {
            log.debug("Write-behind lock held by another instance, skipping");
            return;
        }

        try {
            var today = LocalDate.now();
            log.debug("Starting write-behind persistence for {}", today);
            snapshotDayToDb(today);
            log.debug("Write-behind persistence completed for {}", today);
        } catch (Exception e) {
            log.error("Write-behind persistence failed: {}", e.getMessage(), e);
        } finally {
            releaseWriteBehindLock();
        }
    }

    /**
     * Snapshot yesterday's daily analytics to DynamoDB.
     * NOTE: Now handled by Lambda via EventBridge. Kept for manual invocation.
     */
    public void snapshotDailyAnalytics() {
        if (!isEnabled()) {
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
     */
    public void snapshotDayToDb(LocalDate date) {
        String redisKey = VIEWS_DAILY_PREFIX + date.format(DATE_FORMATTER);
        var viewCounts = readViewCountsFromRedis(redisKey);

        if (viewCounts.isEmpty()) {
            log.debug("No analytics data to snapshot for {}", date);
            return;
        }

        dynamoDbRepository.saveDailySnapshot(date, viewCounts);
        log.info("Persisted {} analytics entries for {} to DynamoDB", viewCounts.size(), date);
    }

    /**
     * Snapshot monthly aggregated analytics to DynamoDB.
     *
     * @return map of mediaId to view count (for S3 archival)
     */
    public Map<String, Long> snapshotMonthToDb(String yearMonth) {
        var aggregatedViews = dynamoDbRepository.aggregateMonthFromDaily(yearMonth);

        if (!aggregatedViews.isEmpty()) {
            dynamoDbRepository.saveMonthlySnapshot(yearMonth, aggregatedViews);
        }

        return aggregatedViews;
    }

    /**
     * Archive last month's analytics to S3.
     * NOTE: Now handled by Lambda via EventBridge. Kept for manual invocation.
     */
    public void archiveMonthlyAnalyticsToS3() {
        if (!s3ArchiveService.isEnabled()) {
            return;
        }

        try {
            var lastMonth = YearMonth.now().minusMonths(1);
            String yearMonth = lastMonth.format(DateTimeFormatter.ofPattern("yyyy-MM"));

            var monthlyData = snapshotMonthToDb(yearMonth);
            if (!monthlyData.isEmpty()) {
                s3ArchiveService.archive(yearMonth, monthlyData);
            }

            log.info("Monthly analytics archive completed for {}", yearMonth);
        } catch (Exception e) {
            log.error("Failed to archive monthly analytics to S3: {}", e.getMessage(), e);
        }
    }

    /**
     * Archive analytics data to S3.
     */
    public void archiveToS3(String yearMonth, Map<String, Long> data) {
        s3ArchiveService.archive(yearMonth, data);
    }

    /**
     * Restore Redis analytics data from DynamoDB.
     */
    public void restoreFromDb(LocalDate date) {
        String redisKey = VIEWS_DAILY_PREFIX + date.format(DATE_FORMATTER);
        var viewCounts = dynamoDbRepository.queryAnalytics("DAILY", date.format(DATE_FORMATTER));

        if (viewCounts.isEmpty()) {
            log.debug("No persisted analytics data found for {}", date);
            return;
        }

        for (var entry : viewCounts.entrySet()) {
            redisTemplate.opsForZSet().add(redisKey, entry.getKey(), entry.getValue());
            redisTemplate.opsForZSet().incrementScore(VIEWS_TOTAL_KEY, entry.getKey(), entry.getValue());
        }

        int retentionDays = analyticsProperties.getPersistence().getDailyRetentionDays();
        redisTemplate.expire(redisKey, retentionDays, TimeUnit.DAYS);
        log.info("Restored {} analytics entries for {} from DynamoDB", viewCounts.size(), date);
    }

    /**
     * Aggregate daily analytics over a date range from DynamoDB.
     * Used for calculating weekly/monthly/yearly views at read-time.
     */
    public Map<String, Long> aggregateDailyAnalytics(LocalDate startDate, LocalDate endDate, int limit) {
        return dynamoDbRepository.aggregateDailyRange(startDate, endDate, limit);
    }

    /**
     * Get historical analytics for a specific period from DynamoDB.
     */
    public Map<String, Long> getHistoricalAnalytics(String period, String dateKey, int limit) {
        var results = dynamoDbRepository.queryAnalytics(period, dateKey);

        // Sort and limit
        return results.entrySet().stream()
                .sorted((a, b) -> Long.compare(b.getValue(), a.getValue()))
                .limit(limit)
                .collect(java.util.stream.Collectors.toMap(
                        Map.Entry::getKey,
                        Map.Entry::getValue,
                        (e1, e2) -> e1,
                        LinkedHashMap::new));
    }

    private boolean isEnabled() {
        return analyticsProperties.isEnabled() && analyticsProperties.getPersistence().isEnabled();
    }

    private Map<String, Long> readViewCountsFromRedis(String redisKey) {
        var viewCounts = new HashMap<String, Long>();
        var entries = redisTemplate.opsForZSet().rangeWithScores(redisKey, 0, -1);

        if (entries != null) {
            for (var entry : entries) {
                String mediaId = entry.getValue();
                long viewCount = entry.getScore() != null ? entry.getScore().longValue() : 0;
                if (mediaId != null && viewCount > 0) {
                    viewCounts.put(mediaId, viewCount);
                }
            }
        }

        return viewCounts;
    }

    private boolean acquireWriteBehindLock() {
        try {
            Boolean acquired = redisTemplate.opsForValue().setIfAbsent(
                    WRITE_BEHIND_LOCK_KEY, "locked", WRITE_BEHIND_LOCK_TTL);
            return Boolean.TRUE.equals(acquired);
        } catch (Exception e) {
            log.warn("Failed to acquire write-behind lock: {}", e.getMessage());
            return false;
        }
    }

    private void releaseWriteBehindLock() {
        try {
            redisTemplate.delete(WRITE_BEHIND_LOCK_KEY);
        } catch (Exception e) {
            log.warn("Failed to release write-behind lock: {}", e.getMessage());
        }
    }
}
