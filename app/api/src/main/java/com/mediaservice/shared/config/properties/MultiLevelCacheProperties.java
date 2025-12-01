package com.mediaservice.shared.config.properties;

import lombok.Getter;
import lombok.Setter;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

/**
 * Configuration properties for multi-level caching architecture.
 *
 * <p>
 * Supports three-level caching:
 * <ul>
 * <li>L1: Local Caffeine cache (per-instance, microsecond access)</li>
 * <li>L2: Redis distributed cache (shared across instances)</li>
 * <li>L3: Database (DynamoDB)</li>
 * </ul>
 *
 * <p>
 * Additional features:
 * <ul>
 * <li>Hotkey detection: Extends TTL for frequently accessed items</li>
 * <li>Single-flight: Prevents thundering herd on cache miss</li>
 * <li>TTL jitter: Prevents cache avalanche</li>
 * </ul>
 */
@Component
@ConfigurationProperties(prefix = "multi-level-cache")
@Getter
@Setter
public class MultiLevelCacheProperties {
  private boolean enabled = true;
  private LocalCacheConfig local = new LocalCacheConfig();
  private HotkeyConfig hotkey = new HotkeyConfig();
  private SingleFlightConfig singleFlight = new SingleFlightConfig();
  private JitterConfig jitter = new JitterConfig();

  /**
   * L1 local cache configuration (Caffeine).
   */
  @Getter
  @Setter
  public static class LocalCacheConfig {
    private boolean enabled = true;
    /** Maximum number of entries in L1 cache */
    private int maxSize = 1000;
    /** TTL in seconds for L1 cache (should be shorter than L2) */
    private int ttlSeconds = 30;
    /** Whether to record cache statistics for monitoring */
    private boolean recordStats = true;
  }

  /**
   * Hotkey detection configuration.
   */
  @Getter
  @Setter
  public static class HotkeyConfig {
    private boolean enabled = true;
    /** Access count threshold to be considered a hotkey (per window) */
    private int threshold = 100;
    /** Time window in seconds for hotkey detection */
    private int windowSeconds = 60;
    /** TTL multiplier for hot keys */
    private int ttlMultiplier = 3;
    /** Redis key prefix for hotkey counters */
    private String keyPrefix = "hotkey:counter:";
    /** Redis key for known hot keys set */
    private String knownHotKeysKey = "hotkey:known";
    /** Warm-up configuration for loading from analytics */
    private WarmUpConfig warmUp = new WarmUpConfig();
  }

  /**
   * Warm-up configuration for hotkey detection.
   * Loads known hot keys from analytics data on startup and periodically.
   */
  @Getter
  @Setter
  public static class WarmUpConfig {
    private boolean enabled = true;
    /** Number of top items to load from analytics */
    private int topCount = 100;
    /** Refresh interval in minutes for reloading from analytics */
    private int refreshIntervalMinutes = 5;
    /** TTL in minutes for known hot keys set */
    private int knownKeysTtlMinutes = 30;
  }

  /**
   * Single-flight (singleflight) configuration for preventing thundering herd.
   */
  @Getter
  @Setter
  public static class SingleFlightConfig {
    private boolean enabled = true;
    /** Lock TTL in milliseconds (should be longer than typical DB query) */
    private int lockTtlMs = 5000;
    /** Maximum retries while waiting for cache to be populated */
    private int maxRetries = 50;
    /** Sleep interval in milliseconds between retries */
    private int retryIntervalMs = 100;
    /** Redis key prefix for single-flight locks */
    private String lockPrefix = "singleflight:";
  }

  /**
   * TTL jitter configuration for preventing cache avalanche.
   */
  @Getter
  @Setter
  public static class JitterConfig {
    private boolean enabled = true;
    /** Maximum jitter percentage (0.0 to 1.0). 0.1 means ±10% */
    private double maxPercent = 0.1;
  }
}
