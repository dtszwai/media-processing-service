package com.mediaservice.service;

import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.config.AnalyticsProperties;
import com.mediaservice.dto.analytics.*;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.data.redis.core.RedisCallback;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

import java.time.LocalDate;
import java.time.format.DateTimeFormatter;
import java.time.temporal.TemporalAdjusters;
import java.util.*;
import java.util.concurrent.TimeUnit;

/**
 * Service for tracking and querying analytics data using Redis.
 *
 * <p>
 * Implements the "Roll-Up Pattern" for scalable analytics:
 * <ul>
 * <li>Write: Only increment daily bucket + all-time total (2 writes per
 * view)</li>
 * <li>Read: TODAY/ALL_TIME from Redis; WEEK/MONTH/YEAR aggregated from
 * DynamoDB</li>
 * <li>Persist: Write-behind every 5 minutes for durability (not 1 AM cron)</li>
 * </ul>
 *
 * <p>
 * Redis Key Design:
 * <ul>
 * <li>{@code views:daily:{YYYY-MM-DD}} - Daily view counts (Sorted Set)</li>
 * <li>{@code views:total} - All-time view counts (Sorted Set)</li>
 * <li>{@code media:{mediaId}:views} - Total views per media (String
 * counter)</li>
 * <li>{@code analytics:formats:{YYYY-MM-DD}} - Daily format usage (Hash)</li>
 * <li>{@code analytics:downloads:{YYYY-MM-DD}} - Daily download counts
 * (Hash)</li>
 * </ul>
 *
 * <p>
 * Weekly/Monthly/Yearly views are calculated at read-time by summing daily
 * snapshots from DynamoDB, not stored as separate Redis keys.
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class AnalyticsService {
  private final StringRedisTemplate redisTemplate;
  private final AnalyticsProperties analyticsProperties;
  private final DynamoDbService dynamoDbService;
  private final AnalyticsPersistenceService persistenceService;

  // Redis key prefixes - only daily + total (Roll-Up Pattern)
  private static final String VIEWS_DAILY_PREFIX = "views:daily:";
  private static final String VIEWS_TOTAL_KEY = "views:total";
  private static final String MEDIA_VIEWS_PREFIX = "media:views:";
  private static final String FORMAT_USAGE_PREFIX = "analytics:formats:";
  private static final String DOWNLOADS_PREFIX = "analytics:downloads:";

  private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;

  // TTL: Keep daily data in Redis for 25-26 hours (allows overlap for
  // persistence)
  // Historical weekly/monthly data is aggregated from DynamoDB, not Redis
  private static final long DAILY_TTL_HOURS = 26;

  /**
   * Record a view for a media item. Called asynchronously to not block downloads.
   *
   * <p>
   * Implements Roll-Up Pattern: Only writes to daily bucket + all-time total.
   * Weekly/monthly/yearly are calculated at read-time from DynamoDB snapshots.
   * This reduces write amplification from 5 writes to 2 writes per view.
   *
   * @param mediaId The media ID that was viewed
   */
  @Async
  public void recordView(String mediaId) {
    if (!analyticsProperties.isEnabled()) {
      return;
    }
    try {
      LocalDate today = LocalDate.now();
      String dailyKey = VIEWS_DAILY_PREFIX + today.format(DATE_FORMATTER);
      String mediaKey = MEDIA_VIEWS_PREFIX + mediaId;

      // Roll-Up Pattern: Only increment daily bucket + all-time total
      // Weekly/monthly/yearly are aggregated from DynamoDB at read-time
      redisTemplate.executePipelined((RedisCallback<Object>) connection -> {
        var stringConn = connection.stringCommands();
        var zSetConn = connection.zSetCommands();

        // 1. Daily bucket (for today's leaderboard)
        zSetConn.zIncrBy(dailyKey.getBytes(), 1, mediaId.getBytes());

        // 2. All-time total (for quick all-time queries)
        zSetConn.zIncrBy(VIEWS_TOTAL_KEY.getBytes(), 1, mediaId.getBytes());

        // 3. Per-media counter (for individual media stats)
        stringConn.incr(mediaKey.getBytes());

        // TTL: 26 hours ensures data survives until next persistence cycle
        connection.keyCommands().expire(dailyKey.getBytes(), TimeUnit.HOURS.toSeconds(DAILY_TTL_HOURS));
        return null;
      });
      log.debug("Recorded view for mediaId: {}", mediaId);
    } catch (Exception e) {
      log.error("Failed to record view for mediaId {}: {}", mediaId, e.getMessage());
    }
  }

  /**
   * Record a download event including format information.
   * Uses Redis pipelining to batch all commands into a single network round-trip.
   *
   * @param mediaId The media ID that was downloaded
   * @param format  The output format (JPEG, PNG, WebP)
   * @param width   The resize width
   */
  @Async
  public void recordDownload(String mediaId, OutputFormat format, Integer width) {
    if (!analyticsProperties.isEnabled()) {
      return;
    }

    try {
      String dateKey = LocalDate.now().format(DATE_FORMATTER);
      String formatKey = FORMAT_USAGE_PREFIX + dateKey;
      String downloadKey = DOWNLOADS_PREFIX + dateKey;
      String formatName = format != null ? format.getFormat().toUpperCase() : "UNKNOWN";

      // Use pipelining to batch all Redis commands into a single round-trip
      redisTemplate.executePipelined((RedisCallback<Object>) connection -> {
        var hashConn = connection.hashCommands();

        // Increment format usage
        hashConn.hIncrBy(formatKey.getBytes(), formatName.getBytes(), 1);
        hashConn.hIncrBy(formatKey.getBytes(), "TOTAL".getBytes(), 1);

        // Increment download count
        hashConn.hIncrBy(downloadKey.getBytes(), "total".getBytes(), 1);
        hashConn.hIncrBy(downloadKey.getBytes(), ("format:" + formatName).getBytes(), 1);

        // Set TTLs (26 hours for persistence window)
        connection.keyCommands().expire(formatKey.getBytes(), TimeUnit.HOURS.toSeconds(DAILY_TTL_HOURS));
        connection.keyCommands().expire(downloadKey.getBytes(), TimeUnit.HOURS.toSeconds(DAILY_TTL_HOURS));

        return null;
      });

      log.debug("Recorded download for mediaId: {}, format: {}", mediaId, formatName);
    } catch (Exception e) {
      log.error("Failed to record download for mediaId {}: {}", mediaId, e.getMessage());
    }
  }

  /**
   * Get total view count for a specific media item.
   *
   * @param mediaId The media ID
   * @return Total view count
   */
  public long getViewCount(String mediaId) {
    try {
      var value = redisTemplate.opsForValue().get(MEDIA_VIEWS_PREFIX + mediaId);
      return value != null ? Long.parseLong(value) : 0;
    } catch (Exception e) {
      log.error("Failed to get view count for mediaId {}: {}", mediaId, e.getMessage());
      return 0;
    }
  }

  /**
   * Get view statistics for a specific media item.
   * Today and total from Redis; week/month/year aggregated from DynamoDB.
   *
   * @param mediaId The media ID
   * @return View statistics across different time periods
   */
  public ViewStats getMediaViews(String mediaId) {
    var today = LocalDate.now();

    // Today's views from Redis (fast)
    long todayViews = getScoreFromZSet(VIEWS_DAILY_PREFIX + today.format(DATE_FORMATTER), mediaId);

    // Week/Month/Year from DynamoDB aggregation (Roll-Up Pattern)
    var weekStart = today.with(TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY));
    var monthStart = today.withDayOfMonth(1);
    var yearStart = today.withDayOfYear(1);

    // Aggregate from persisted daily snapshots
    long weekViews = getAggregatedViewsForMedia(mediaId, weekStart, today) + todayViews;
    long monthViews = getAggregatedViewsForMedia(mediaId, monthStart, today) + todayViews;
    long yearViews = getAggregatedViewsForMedia(mediaId, yearStart, today) + todayViews;

    return ViewStats.builder()
        .mediaId(mediaId)
        .total(getViewCount(mediaId))
        .today(todayViews)
        .thisWeek(weekViews)
        .thisMonth(monthViews)
        .thisYear(yearViews)
        .build();
  }

  /**
   * Get aggregated views for a specific media from DynamoDB daily snapshots.
   */
  private long getAggregatedViewsForMedia(String mediaId, LocalDate startDate, LocalDate endDate) {
    // Query DynamoDB for daily snapshots in range (excluding today, which is in
    // Redis)
    var aggregated = persistenceService.aggregateDailyAnalytics(startDate, endDate.minusDays(1), Integer.MAX_VALUE);
    return aggregated.getOrDefault(mediaId, 0L);
  }

  /**
   * Get top media by views for a specific time period.
   * TODAY and ALL_TIME from Redis; WEEK/MONTH/YEAR aggregated from DynamoDB.
   *
   * @param period Time period (TODAY, THIS_WEEK, THIS_MONTH, THIS_YEAR, ALL_TIME)
   * @param limit  Maximum number of results
   * @return List of media with view counts, ranked by views
   */
  public List<MediaViewCount> getTopMedia(Period period, int limit) {
    try {
      Map<String, Long> viewCounts;

      switch (period) {
        case TODAY -> {
          // Fast path: directly from Redis daily key
          viewCounts = getTopFromRedis(VIEWS_DAILY_PREFIX + LocalDate.now().format(DATE_FORMATTER), limit);
        }
        case ALL_TIME -> {
          // Fast path: directly from Redis all-time key
          viewCounts = getTopFromRedis(VIEWS_TOTAL_KEY, limit);
        }
        case THIS_WEEK, THIS_MONTH, THIS_YEAR -> {
          // Roll-Up Pattern: aggregate from DynamoDB + add today's Redis data
          var today = LocalDate.now();
          LocalDate startDate = switch (period) {
            case THIS_WEEK -> today.with(TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY));
            case THIS_MONTH -> today.withDayOfMonth(1);
            case THIS_YEAR -> today.withDayOfYear(1);
            default -> today;
          };

          // Get historical data from DynamoDB (excludes today)
          viewCounts = new HashMap<>(
              persistenceService.aggregateDailyAnalytics(startDate, today.minusDays(1), limit * 2));

          // Add today's data from Redis
          var todayData = getTopFromRedis(VIEWS_DAILY_PREFIX + today.format(DATE_FORMATTER), limit * 2);
          for (var entry : todayData.entrySet()) {
            viewCounts.merge(entry.getKey(), entry.getValue(), Long::sum);
          }

          // Re-sort and limit
          viewCounts = viewCounts.entrySet().stream()
              .sorted((a, b) -> Long.compare(b.getValue(), a.getValue()))
              .limit(limit)
              .collect(java.util.stream.Collectors.toMap(
                  Map.Entry::getKey,
                  Map.Entry::getValue,
                  (e1, e2) -> e1,
                  LinkedHashMap::new));
        }
        default -> {
          return Collections.emptyList();
        }
      }

      if (viewCounts.isEmpty()) {
        return Collections.emptyList();
      }

      // Build result with media names
      var result = new ArrayList<MediaViewCount>();
      int rank = 1;
      for (var entry : viewCounts.entrySet()) {
        var name = dynamoDbService.getMedia(entry.getKey())
            .map(media -> media.getName())
            .orElse("Unknown");
        result.add(MediaViewCount.builder()
            .mediaId(entry.getKey())
            .name(name)
            .viewCount(entry.getValue())
            .rank(rank++)
            .build());
      }
      return result;
    } catch (Exception e) {
      log.error("Failed to get top media for period {}: {}", period, e.getMessage());
      return Collections.emptyList();
    }
  }

  /**
   * Get top entries from a Redis sorted set.
   */
  private Map<String, Long> getTopFromRedis(String key, int limit) {
    var results = new LinkedHashMap<String, Long>();
    var topEntries = redisTemplate.opsForZSet().reverseRangeWithScores(key, 0, limit - 1);
    if (topEntries != null) {
      for (var entry : topEntries) {
        if (entry.getValue() != null && entry.getScore() != null) {
          results.put(entry.getValue(), entry.getScore().longValue());
        }
      }
    }
    return results;
  }

  /**
   * Get format usage statistics.
   *
   * @param period Time period
   * @return Format usage statistics
   */
  public FormatUsageStats getFormatUsage(Period period) {
    var usage = new HashMap<String, Long>();
    long total = 0;
    try {
      var keys = getKeysForPeriodRange(FORMAT_USAGE_PREFIX, period);
      for (var key : keys) {
        var dayUsage = redisTemplate.opsForHash().entries(key);
        for (var entry : dayUsage.entrySet()) {
          var format = entry.getKey().toString();
          if (!"TOTAL".equals(format)) {
            long count = Long.parseLong(entry.getValue().toString());
            usage.merge(format, count, Long::sum);
            total += count;
          }
        }
      }
    } catch (Exception e) {
      log.error("Failed to get format usage for period {}: {}", period, e.getMessage());
    }
    return FormatUsageStats.builder()
        .period(period)
        .usage(usage)
        .total(total)
        .build();
  }

  /**
   * Get download statistics.
   *
   * @param period Time period
   * @return Download statistics
   */
  public DownloadStats getDownloadStats(Period period) {
    var byFormat = new HashMap<String, Long>();
    var byDay = new LinkedHashMap<String, Long>();
    long totalDownloads = 0;
    try {
      var keys = getKeysForPeriodRange(DOWNLOADS_PREFIX, period);
      for (var key : keys) {
        var dateKey = key.replace(DOWNLOADS_PREFIX, "");
        var dayStats = redisTemplate.opsForHash().entries(key);
        long dayTotal = 0;
        for (var entry : dayStats.entrySet()) {
          var field = entry.getKey().toString();
          long count = Long.parseLong(entry.getValue().toString());
          if ("total".equals(field)) {
            dayTotal = count;
            totalDownloads += count;
          } else if (field.startsWith("format:")) {
            var format = field.replace("format:", "");
            byFormat.merge(format, count, Long::sum);
          }
        }
        byDay.put(dateKey, dayTotal);
      }
    } catch (Exception e) {
      log.error("Failed to get download stats for period {}: {}", period, e.getMessage());
    }

    return DownloadStats.builder()
        .period(period)
        .totalDownloads(totalDownloads)
        .byFormat(byFormat)
        .byDay(byDay)
        .build();
  }

  /**
   * Get comprehensive analytics summary.
   *
   * @return Analytics summary for dashboard
   */
  public AnalyticsSummary getSummary() {
    var limit = analyticsProperties.getTopMediaLimit();
    // Get today's stats
    var todayDownloads = getDownloadStats(Period.TODAY);
    var formatUsage = getFormatUsage(Period.ALL_TIME);
    // Calculate total views from all-time key
    long totalViews = 0;
    try {
      var size = redisTemplate.opsForZSet().zCard(VIEWS_TOTAL_KEY);
      if (size != null && size > 0) {
        // Sum all scores
        var all = redisTemplate.opsForZSet().rangeWithScores(VIEWS_TOTAL_KEY, 0, -1);
        if (all != null) {
          totalViews = all.stream()
              .mapToLong(t -> t.getScore() != null ? t.getScore().longValue() : 0)
              .sum();
        }
      }
    } catch (Exception e) {
      log.error("Failed to calculate total views: {}", e.getMessage());
    }
    // Get today's views
    long viewsToday = 0;
    try {
      var todayKey = VIEWS_DAILY_PREFIX + LocalDate.now().format(DATE_FORMATTER);
      var todayViews = redisTemplate.opsForZSet().rangeWithScores(todayKey, 0, -1);
      if (todayViews != null) {
        viewsToday = todayViews.stream()
            .mapToLong(t -> t.getScore() != null ? t.getScore().longValue() : 0)
            .sum();
      }
    } catch (Exception e) {
      log.error("Failed to calculate today's views: {}", e.getMessage());
    }
    return AnalyticsSummary.builder()
        .totalViews(totalViews)
        .totalDownloads(todayDownloads.getTotalDownloads())
        .viewsToday(viewsToday)
        .downloadsToday(todayDownloads.getTotalDownloads())
        .topMediaToday(getTopMedia(Period.TODAY, limit))
        .topMediaAllTime(getTopMedia(Period.ALL_TIME, limit))
        .formatUsage(formatUsage.getUsage())
        .build();
  }

  private List<String> getKeysForPeriodRange(String prefix, Period period) {
    var today = LocalDate.now();
    var keys = new ArrayList<String>();
    switch (period) {
      case TODAY -> keys.add(prefix + today.format(DATE_FORMATTER));
      case THIS_WEEK -> {
        var weekStart = today.with(TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY));
        for (var date = weekStart; !date.isAfter(today); date = date.plusDays(1)) {
          keys.add(prefix + date.format(DATE_FORMATTER));
        }
      }
      case THIS_MONTH -> {
        var monthStart = today.withDayOfMonth(1);
        for (LocalDate date = monthStart; !date.isAfter(today); date = date.plusDays(1)) {
          keys.add(prefix + date.format(DATE_FORMATTER));
        }
      }
      case THIS_YEAR -> {
        var yearStart = today.withDayOfYear(1);
        for (LocalDate date = yearStart; !date.isAfter(today); date = date.plusDays(1)) {
          keys.add(prefix + date.format(DATE_FORMATTER));
        }
      }
      case ALL_TIME -> {
        // Get all keys matching the pattern
        var allKeys = redisTemplate.keys(prefix + "*");
        if (allKeys != null) {
          keys.addAll(allKeys);
        }
      }
    }

    return keys;
  }

  private long getScoreFromZSet(String key, String member) {
    try {
      var score = redisTemplate.opsForZSet().score(key, member);
      return score != null ? score.longValue() : 0;
    } catch (Exception e) {
      return 0;
    }
  }
}
