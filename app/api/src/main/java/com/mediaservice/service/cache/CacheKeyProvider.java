package com.mediaservice.service.cache;

import com.mediaservice.common.model.OutputFormat;

import java.util.Arrays;

/**
 * Centralized cache key generation for all cache layers.
 *
 * <p>
 * This class provides consistent key generation across L1 (Caffeine),
 * L2 (Redis), and ensures cache operations don't have key mismatches.
 *
 * <p>
 * Key naming conventions:
 * <ul>
 * <li>Media metadata: {@code media::{mediaId}}</li>
 * <li>Media status: {@code mediaStatus::{mediaId}}</li>
 * <li>Presigned URL: {@code presignedUrl::{mediaId}:{format}}</li>
 * <li>Hotkey counter: {@code hotkey:counter:{mediaId}}</li>
 * <li>Single-flight lock: {@code singleflight:{mediaId}}</li>
 * </ul>
 */
public final class CacheKeyProvider {

  // Cache name constants (used with Spring CacheManager)
  public static final String CACHE_MEDIA = "media";
  public static final String CACHE_MEDIA_STATUS = "mediaStatus";
  public static final String CACHE_PRESIGNED_URL = "presignedUrl";
  public static final String CACHE_PAGINATION = "pagination";

  // Redis key prefixes for direct Redis operations
  public static final String HOTKEY_COUNTER_PREFIX = "hotkey:counter:";
  public static final String HOTKEY_KNOWN_SET = "hotkey:known";
  public static final String SINGLE_FLIGHT_PREFIX = "singleflight:";

  // Pub/Sub channel
  public static final String CACHE_INVALIDATION_CHANNEL = "cache:invalidate";

  private CacheKeyProvider() {
    // Utility class - prevent instantiation
  }

  /**
   * Get all supported output formats as uppercase strings.
   *
   * <p>
   * Used for cache invalidation to clear all format variants.
   *
   * @return Array of format strings (e.g., ["JPEG", "PNG", "WEBP"])
   */
  public static String[] getAllFormats() {
    return Arrays.stream(OutputFormat.values())
        .map(f -> f.getFormat().toUpperCase())
        .toArray(String[]::new);
  }

  /**
   * Generate cache key for media metadata.
   *
   * @param mediaId The media ID
   * @return Cache key for media metadata
   */
  public static String mediaKey(String mediaId) {
    return mediaId;
  }

  /**
   * Generate cache key for presigned URL.
   *
   * @param mediaId The media ID
   * @param format  The output format
   * @return Cache key for presigned URL
   */
  public static String presignedUrlKey(String mediaId, String format) {
    return mediaId + ":" + format.toUpperCase();
  }

  /**
   * Generate cache key for presigned URL using OutputFormat.
   *
   * @param mediaId The media ID
   * @param format  The output format enum
   * @return Cache key for presigned URL
   */
  public static String presignedUrlKey(String mediaId, OutputFormat format) {
    return presignedUrlKey(mediaId, format.getFormat());
  }

  /**
   * Generate Redis key for hotkey counter.
   *
   * @param mediaId The media ID
   * @return Redis key for hotkey counter
   */
  public static String hotkeyCounterKey(String mediaId) {
    return HOTKEY_COUNTER_PREFIX + mediaId;
  }

  /**
   * Generate Redis key for single-flight lock.
   *
   * @param mediaId The media ID
   * @return Redis key for single-flight lock
   */
  public static String singleFlightLockKey(String mediaId) {
    return SINGLE_FLIGHT_PREFIX + mediaId;
  }

  /**
   * Generate all presigned URL cache keys for a media item.
   *
   * @param mediaId The media ID
   * @return Array of presigned URL cache keys for all formats
   */
  public static String[] allPresignedUrlKeys(String mediaId) {
    return Arrays.stream(getAllFormats())
        .map(format -> presignedUrlKey(mediaId, format))
        .toArray(String[]::new);
  }
}
