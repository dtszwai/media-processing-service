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
 * Provides view counting, leaderboards, and format usage analytics.
 * Uses Redis Sorted Sets for efficient ranking operations.
 *
 * <p>
 * Redis Key Design:
 * <ul>
 * <li>{@code views:daily:{YYYY-MM-DD}} - Daily view counts (Sorted Set)</li>
 * <li>{@code views:weekly:{YYYY-Www}} - Weekly view counts (Sorted Set)</li>
 * <li>{@code views:monthly:{YYYY-MM}} - Monthly view counts (Sorted Set)</li>
 * <li>{@code views:total} - All-time view counts (Sorted Set)</li>
 * <li>{@code media:{mediaId}:views} - Total views per media (String
 * counter)</li>
 * <li>{@code analytics:formats:{YYYY-MM-DD}} - Daily format usage (Hash)</li>
 * <li>{@code analytics:downloads:{YYYY-MM-DD}} - Daily download counts
 * (Hash)</li>
 * </ul>
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class AnalyticsService {
  private final StringRedisTemplate redisTemplate;
  private final AnalyticsProperties analyticsProperties;
  private final DynamoDbService dynamoDbService;

  private static final String VIEWS_DAILY_PREFIX = "views:daily:";
  private static final String VIEWS_WEEKLY_PREFIX = "views:weekly:";
  private static final String VIEWS_MONTHLY_PREFIX = "views:monthly:";
  private static final String VIEWS_TOTAL_KEY = "views:total";
  private static final String MEDIA_VIEWS_PREFIX = "media:views:";
  private static final String FORMAT_USAGE_PREFIX = "analytics:formats:";
  private static final String DOWNLOADS_PREFIX = "analytics:downloads:";

  private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;
  private static final DateTimeFormatter WEEK_FORMATTER = DateTimeFormatter.ofPattern("yyyy-'W'ww");
  private static final DateTimeFormatter MONTH_FORMATTER = DateTimeFormatter.ofPattern("yyyy-MM");

  // TTL constants
  private static final long DAILY_TTL_DAYS = 90;
  private static final long WEEKLY_TTL_DAYS = 365;
  private static final long MONTHLY_TTL_DAYS = 730;

  /**
   * Record a view for a media item. Called asynchronously to not block downloads.
   * Uses Redis pipelining to batch all increments into a single network
   * round-trip.
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
      String weeklyKey = VIEWS_WEEKLY_PREFIX + today.format(WEEK_FORMATTER);
      String monthlyKey = VIEWS_MONTHLY_PREFIX + today.format(MONTH_FORMATTER);
      String mediaKey = MEDIA_VIEWS_PREFIX + mediaId;
      // Use pipelining to batch all Redis commands into a single round-trip
      redisTemplate.executePipelined((RedisCallback<Object>) connection -> {
        var stringConn = connection.stringCommands();
        var zSetConn = connection.zSetCommands();
        // Increment all counters
        zSetConn.zIncrBy(dailyKey.getBytes(), 1, mediaId.getBytes());
        zSetConn.zIncrBy(weeklyKey.getBytes(), 1, mediaId.getBytes());
        zSetConn.zIncrBy(monthlyKey.getBytes(), 1, mediaId.getBytes());
        zSetConn.zIncrBy(VIEWS_TOTAL_KEY.getBytes(), 1, mediaId.getBytes());
        stringConn.incr(mediaKey.getBytes());
        // Set TTLs (only if key doesn't have one)
        connection.keyCommands().expire(dailyKey.getBytes(), TimeUnit.DAYS.toSeconds(DAILY_TTL_DAYS));
        connection.keyCommands().expire(weeklyKey.getBytes(), TimeUnit.DAYS.toSeconds(WEEKLY_TTL_DAYS));
        connection.keyCommands().expire(monthlyKey.getBytes(), TimeUnit.DAYS.toSeconds(MONTHLY_TTL_DAYS));
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

        // Set TTLs
        connection.keyCommands().expire(formatKey.getBytes(), TimeUnit.DAYS.toSeconds(DAILY_TTL_DAYS));
        connection.keyCommands().expire(downloadKey.getBytes(), TimeUnit.DAYS.toSeconds(DAILY_TTL_DAYS));

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
   *
   * @param mediaId The media ID
   * @return View statistics across different time periods
   */
  public ViewStats getMediaViews(String mediaId) {
    var today = LocalDate.now();
    return ViewStats.builder()
        .mediaId(mediaId)
        .total(getViewCount(mediaId))
        .today(getScoreFromZSet(VIEWS_DAILY_PREFIX + today.format(DATE_FORMATTER), mediaId))
        .thisWeek(getScoreFromZSet(VIEWS_WEEKLY_PREFIX + today.format(WEEK_FORMATTER), mediaId))
        .thisMonth(getScoreFromZSet(VIEWS_MONTHLY_PREFIX + today.format(MONTH_FORMATTER), mediaId))
        .build();
  }

  /**
   * Get top media by views for a specific time period.
   *
   * @param period Time period (TODAY, THIS_WEEK, THIS_MONTH, ALL_TIME)
   * @param limit  Maximum number of results
   * @return List of media with view counts, ranked by views
   */
  public List<MediaViewCount> getTopMedia(Period period, int limit) {
    var key = getKeyForPeriod(period);
    if (key == null) {
      return Collections.emptyList();
    }
    try {
      var topEntries = redisTemplate.opsForZSet().reverseRangeWithScores(key, 0,
          limit - 1);
      if (topEntries == null || topEntries.isEmpty()) {
        return Collections.emptyList();
      }
      var result = new ArrayList<MediaViewCount>();
      int rank = 1;
      for (var entry : topEntries) {
        String mediaId = entry.getValue();
        long viewCount = entry.getScore() != null ? entry.getScore().longValue() : 0;
        // Fetch media name from database
        var name = dynamoDbService.getMedia(mediaId)
            .map(media -> media.getName())
            .orElse("Unknown");
        result.add(MediaViewCount.builder()
            .mediaId(mediaId)
            .name(name)
            .viewCount(viewCount)
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

  private String getKeyForPeriod(Period period) {
    var today = LocalDate.now();
    return switch (period) {
      case TODAY -> VIEWS_DAILY_PREFIX + today.format(DATE_FORMATTER);
      case THIS_WEEK -> VIEWS_WEEKLY_PREFIX + today.format(WEEK_FORMATTER);
      case THIS_MONTH -> VIEWS_MONTHLY_PREFIX + today.format(MONTH_FORMATTER);
      case ALL_TIME -> VIEWS_TOTAL_KEY;
    };
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
