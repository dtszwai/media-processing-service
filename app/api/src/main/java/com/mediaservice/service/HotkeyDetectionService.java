package com.mediaservice.service;

import com.mediaservice.config.MultiLevelCacheProperties;
import com.mediaservice.dto.analytics.Period;
import jakarta.annotation.PostConstruct;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.util.Set;
import java.util.concurrent.TimeUnit;

/**
 * Service for detecting and tracking hot keys (frequently accessed items).
 *
 * <p>
 * Implements a hybrid approach combining:
 * <ul>
 * <li><b>Real-time detection</b>: Sliding window counter per key (60s
 * window)</li>
 * <li><b>Analytics warm-up</b>: Pre-load known hot keys from analytics
 * data</li>
 * </ul>
 *
 * <p>
 * This hybrid approach provides:
 * <ul>
 * <li>Fast detection of new viral content (real-time counter)</li>
 * <li>No cold-start problem (warm-up from analytics)</li>
 * <li>Reduced Redis writes compared to counter-only approach</li>
 * </ul>
 *
 * <p>
 * A key is considered "hot" if EITHER:
 * <ul>
 * <li>Real-time counter exceeds threshold (100 requests in 60s), OR</li>
 * <li>Key exists in known hot keys set (loaded from analytics)</li>
 * </ul>
 */
@Service
@Slf4j
public class HotkeyDetectionService {
  private final StringRedisTemplate stringRedisTemplate;
  private final MultiLevelCacheProperties cacheProperties;
  private final ObjectProvider<AnalyticsService> analyticsServiceProvider;

  public HotkeyDetectionService(
      StringRedisTemplate stringRedisTemplate,
      MultiLevelCacheProperties cacheProperties,
      ObjectProvider<AnalyticsService> analyticsServiceProvider) {
    this.stringRedisTemplate = stringRedisTemplate;
    this.cacheProperties = cacheProperties;
    this.analyticsServiceProvider = analyticsServiceProvider;
  }

  /**
   * Warm up known hot keys from analytics on application startup.
   */
  @PostConstruct
  public void warmUp() {
    if (!isWarmUpEnabled()) {
      log.info("Hotkey warm-up is disabled");
      return;
    }
    try {
      refreshKnownHotKeysFromAnalytics();
      log.info("Hotkey warm-up completed on startup");
    } catch (Exception e) {
      log.warn("Hotkey warm-up failed on startup: {}", e.getMessage());
    }
  }

  /**
   * Periodically refresh known hot keys from analytics.
   * Default: every 5 minutes.
   */
  @Scheduled(fixedRateString = "${multi-level-cache.hotkey.warm-up.refresh-interval-minutes:5}", timeUnit = TimeUnit.MINUTES, initialDelayString = "${multi-level-cache.hotkey.warm-up.refresh-interval-minutes:5}")
  public void scheduledRefresh() {
    if (!isWarmUpEnabled()) {
      return;
    }
    try {
      refreshKnownHotKeysFromAnalytics();
      log.debug("Hotkey scheduled refresh completed");
    } catch (Exception e) {
      log.warn("Hotkey scheduled refresh failed: {}", e.getMessage());
    }
  }

  /**
   * Refresh known hot keys from analytics data.
   *
   * <p>
   * Loads top N items from:
   * <ul>
   * <li>Today's top viewed media</li>
   * <li>All-time top viewed media</li>
   * </ul>
   */
  public void refreshKnownHotKeysFromAnalytics() {
    var analyticsService = analyticsServiceProvider.getIfAvailable();
    if (analyticsService == null) {
      log.debug("AnalyticsService not available for hotkey warm-up");
      return;
    }
    var config = cacheProperties.getHotkey();
    var warmUpConfig = config.getWarmUp();
    String knownHotKeysKey = config.getKnownHotKeysKey();
    try {
      // Get top items from today and all-time
      var todayTop = analyticsService.getTopMedia(Period.TODAY, warmUpConfig.getTopCount());
      var allTimeTop = analyticsService.getTopMedia(Period.ALL_TIME, warmUpConfig.getTopCount());
      // Collect all media IDs
      var hotMediaIds = new java.util.HashSet<String>();
      todayTop.forEach(item -> hotMediaIds.add(item.getMediaId()));
      allTimeTop.forEach(item -> hotMediaIds.add(item.getMediaId()));
      if (hotMediaIds.isEmpty()) {
        log.debug("No hot keys found in analytics");
        return;
      }
      // Use a temporary key to atomically replace the set
      String tempKey = knownHotKeysKey + ":temp:" + System.currentTimeMillis();
      // Add all hot media IDs to temporary set
      stringRedisTemplate.opsForSet().add(tempKey, hotMediaIds.toArray(new String[0]));
      stringRedisTemplate.expire(tempKey, Duration.ofMinutes(warmUpConfig.getKnownKeysTtlMinutes()));
      // Atomically rename to replace the old set
      try {
        stringRedisTemplate.rename(tempKey, knownHotKeysKey);
      } catch (Exception e) {
        // If rename fails (e.g., temp key expired), just delete it
        stringRedisTemplate.delete(tempKey);
        throw e;
      }
      // Set TTL on the new set
      stringRedisTemplate.expire(knownHotKeysKey, Duration.ofMinutes(warmUpConfig.getKnownKeysTtlMinutes()));
      log.info("Refreshed known hot keys: {} items from analytics", hotMediaIds.size());
    } catch (Exception e) {
      log.warn("Failed to refresh known hot keys from analytics: {}", e.getMessage());
    }
  }

  /**
   * Record an access to a key for hotkey detection.
   *
   * <p>
   * Increments a counter in Redis that expires after the configured window.
   * This is a fire-and-forget operation - failures are logged but don't affect
   * the main request flow.
   *
   * @param key The cache key being accessed
   */
  public void recordAccess(String key) {
    if (!cacheProperties.isEnabled() || !cacheProperties.getHotkey().isEnabled()) {
      return;
    }
    try {
      String counterKey = cacheProperties.getHotkey().getKeyPrefix() + key;
      Long count = stringRedisTemplate.opsForValue().increment(counterKey);
      // Set expiry on first increment
      if (count != null && count == 1) {
        stringRedisTemplate.expire(
            counterKey,
            Duration.ofSeconds(cacheProperties.getHotkey().getWindowSeconds()));
      }
    } catch (Exception e) {
      // Non-critical operation - log and continue
      log.debug("Failed to record hotkey access for {}: {}", key, e.getMessage());
    }
  }

  /**
   * Check if a key is considered "hot" (frequently accessed).
   *
   * <p>
   * A key is hot if EITHER:
   * <ul>
   * <li>Real-time counter exceeds threshold, OR</li>
   * <li>Key exists in known hot keys set (from analytics)</li>
   * </ul>
   *
   * @param key The cache key to check
   * @return true if the key is considered hot
   */
  public boolean isHotKey(String key) {
    if (!cacheProperties.isEnabled() || !cacheProperties.getHotkey().isEnabled()) {
      return false;
    }
    // Check 1: Real-time counter
    if (isHotByCounter(key)) {
      return true;
    }
    // Check 2: Known hot keys set (from analytics warm-up)
    if (isKnownHotKey(key)) {
      return true;
    }
    return false;
  }

  /**
   * Check if key is hot based on real-time counter.
   */
  private boolean isHotByCounter(String key) {
    try {
      String counterKey = cacheProperties.getHotkey().getKeyPrefix() + key;
      String countStr = stringRedisTemplate.opsForValue().get(counterKey);
      if (countStr != null) {
        long count = Long.parseLong(countStr);
        return count >= cacheProperties.getHotkey().getThreshold();
      }
    } catch (Exception e) {
      log.debug("Failed to check hotkey counter for {}: {}", key, e.getMessage());
    }

    return false;
  }

  /**
   * Check if key is in the known hot keys set (from analytics warm-up).
   */
  private boolean isKnownHotKey(String key) {
    if (!isWarmUpEnabled()) {
      return false;
    }
    try {
      String knownHotKeysKey = cacheProperties.getHotkey().getKnownHotKeysKey();
      return Boolean.TRUE.equals(stringRedisTemplate.opsForSet().isMember(knownHotKeysKey, key));
    } catch (Exception e) {
      log.debug("Failed to check known hot keys for {}: {}", key, e.getMessage());
    }
    return false;
  }

  /**
   * Get the access count for a key (real-time counter only).
   *
   * @param key The cache key to check
   * @return Current access count, or 0 if not tracked
   */
  public long getAccessCount(String key) {
    try {
      String counterKey = cacheProperties.getHotkey().getKeyPrefix() + key;
      String countStr = stringRedisTemplate.opsForValue().get(counterKey);
      return countStr != null ? Long.parseLong(countStr) : 0;
    } catch (Exception e) {
      log.debug("Failed to get access count for {}: {}", key, e.getMessage());
      return 0;
    }
  }

  /**
   * Calculate the appropriate TTL for a key based on its hotness.
   *
   * <p>
   * Hot keys receive an extended TTL (multiplied by the configured multiplier)
   * to reduce cache misses for frequently accessed content.
   *
   * @param key     The cache key
   * @param baseTtl The base TTL duration
   * @return Extended TTL if hot, otherwise base TTL
   */
  public Duration getTtlForKey(String key, Duration baseTtl) {
    if (isHotKey(key)) {
      int multiplier = cacheProperties.getHotkey().getTtlMultiplier();
      var extendedTtl = baseTtl.multipliedBy(multiplier);
      log.debug("Hot key detected: {}, extending TTL from {} to {}",
          key, baseTtl, extendedTtl);
      return extendedTtl;
    }
    return baseTtl;
  }

  /**
   * Clear the hotkey counter for a specific key.
   *
   * <p>
   * Called when cache is invalidated to reset hotkey tracking.
   *
   * @param key The cache key to clear
   */
  public void clearCounter(String key) {
    try {
      String counterKey = cacheProperties.getHotkey().getKeyPrefix() + key;
      stringRedisTemplate.delete(counterKey);
    } catch (Exception e) {
      log.debug("Failed to clear hotkey counter for {}: {}", key, e.getMessage());
    }
  }

  /**
   * Get all known hot keys from the analytics-loaded set.
   *
   * @return Set of known hot media IDs, or empty set if not available
   */
  public Set<String> getKnownHotKeys() {
    if (!isWarmUpEnabled()) {
      return Set.of();
    }
    try {
      String knownHotKeysKey = cacheProperties.getHotkey().getKnownHotKeysKey();
      var keys = stringRedisTemplate.opsForSet().members(knownHotKeysKey);
      return keys != null ? keys : Set.of();
    } catch (Exception e) {
      log.debug("Failed to get known hot keys: {}", e.getMessage());
      return Set.of();
    }
  }

  /**
   * Get statistics about hotkey detection.
   *
   * @return Statistics string for monitoring
   */
  public String getStats() {
    int knownHotKeysCount = getKnownHotKeys().size();
    return String.format("Hotkey Stats - knownHotKeys=%d, threshold=%d, windowSeconds=%d, ttlMultiplier=%d",
        knownHotKeysCount,
        cacheProperties.getHotkey().getThreshold(),
        cacheProperties.getHotkey().getWindowSeconds(),
        cacheProperties.getHotkey().getTtlMultiplier());
  }

  private boolean isWarmUpEnabled() {
    return cacheProperties.isEnabled()
        && cacheProperties.getHotkey().isEnabled()
        && cacheProperties.getHotkey().getWarmUp().isEnabled();
  }
}
