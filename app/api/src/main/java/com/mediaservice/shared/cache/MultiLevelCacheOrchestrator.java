package com.mediaservice.shared.cache;

import com.mediaservice.common.model.Media;
import com.mediaservice.shared.http.error.CacheLoadTimeoutException;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

import java.util.Optional;
import java.util.function.Supplier;

/**
 * Orchestrator for multi-level cache operations.
 *
 * <p>
 * Coordinates the three-tier cache hierarchy:
 *
 * <pre>
 * Request
 *    │
 *    ▼
 * ┌──────────────────┐
 * │  L1: Caffeine    │  ← Microsecond access, per-instance
 * │  (Local JVM)     │
 * └────────┬─────────┘
 *          │ miss
 *          ▼
 * ┌──────────────────┐
 * │  L2: Redis       │  ← Millisecond access, shared across instances
 * │  (Distributed)   │
 * └────────┬─────────┘
 *          │ miss
 *          ▼
 * ┌──────────────────┐
 * │  L3: DynamoDB    │  ← Source of truth (with single-flight protection)
 * │  (Database)      │
 * └──────────────────┘
 * </pre>
 *
 * <p>
 * Additional features:
 * <ul>
 * <li><b>Single-flight</b>: Prevents thundering herd on cache miss</li>
 * <li><b>Hotkey detection</b>: Extends TTL for frequently accessed items</li>
 * <li><b>TTL jitter</b>: Prevents cache avalanche</li>
 * </ul>
 */
@Service
@Slf4j
public class MultiLevelCacheOrchestrator {
  private final L1CacheManager l1CacheManager;
  private final L2CacheManager l2CacheManager;
  private final HotkeyDetectionService hotkeyService;
  private final SingleFlightService singleFlightService;
  private final MediaDynamoDbRepository dynamoDbService;

  public MultiLevelCacheOrchestrator(
      L1CacheManager l1CacheManager,
      L2CacheManager l2CacheManager,
      HotkeyDetectionService hotkeyService,
      SingleFlightService singleFlightService,
      MediaDynamoDbRepository dynamoDbService) {
    this.l1CacheManager = l1CacheManager;
    this.l2CacheManager = l2CacheManager;
    this.hotkeyService = hotkeyService;
    this.singleFlightService = singleFlightService;
    this.dynamoDbService = dynamoDbService;
  }

  /**
   * Get media from multi-level cache.
   *
   * <p>
   * Lookup order:
   * <ol>
   * <li>L1 (Caffeine) - fastest, per-instance</li>
   * <li>L2 (Redis) - shared across instances</li>
   * <li>L3 (DynamoDB) - with single-flight protection</li>
   * </ol>
   *
   * @param mediaId The media ID to look up
   * @return The media object, or empty if not found
   */
  public Optional<Media> getMedia(String mediaId) {
    // L1: Check local cache (microseconds)
    Optional<Media> l1Result = l1CacheManager.getMedia(mediaId);
    if (l1Result.isPresent()) {
      hotkeyService.recordAccess(mediaId);
      return l1Result;
    }

    // L2: Check Redis cache (milliseconds)
    Optional<Media> l2Result = l2CacheManager.getMedia(mediaId);
    if (l2Result.isPresent()) {
      log.debug("L2 cache hit for mediaId={}", mediaId);
      // Populate L1 cache
      l1CacheManager.putMedia(mediaId, l2Result.get());
      hotkeyService.recordAccess(mediaId);
      return l2Result;
    }

    // L3: Load from database with single-flight protection
    log.debug("Cache miss for mediaId={}, loading from database", mediaId);
    try {
      return singleFlightService.execute(
          mediaId,
          () -> dynamoDbService.getMedia(mediaId),
          this::readMediaFromL2,
          this::writeMediaToCache);
    } catch (CacheLoadTimeoutException e) {
      // Timeout waiting for another instance - fall back to direct load
      log.warn("Single-flight timeout for mediaId={}, loading directly", mediaId);
      Optional<Media> directResult = dynamoDbService.getMedia(mediaId);
      directResult.ifPresent(media -> writeMediaToCache(mediaId, media));
      return directResult;
    }
  }

  /**
   * Get presigned URL from multi-level cache.
   *
   * @param mediaId      The media ID
   * @param format       The output format
   * @param urlGenerator Supplier that generates the presigned URL
   * @return The presigned URL, or empty if media not found
   */
  public Optional<String> getPresignedUrl(String mediaId, String format, Supplier<String> urlGenerator) {
    // L1: Check local cache
    Optional<String> l1Result = l1CacheManager.getPresignedUrl(mediaId, format);
    if (l1Result.isPresent()) {
      return l1Result;
    }

    // L2: Check Redis cache
    Optional<String> l2Result = l2CacheManager.getPresignedUrl(mediaId, format);
    if (l2Result.isPresent()) {
      // Populate L1 cache
      l1CacheManager.putPresignedUrl(mediaId, format, l2Result.get());
      return l2Result;
    }

    // Generate new URL
    String url = urlGenerator.get();
    if (url != null) {
      // Write to both cache levels
      l2CacheManager.putPresignedUrl(mediaId, format, url);
      l1CacheManager.putPresignedUrl(mediaId, format, url);
      log.debug("Generated and cached presigned URL for mediaId={}, format={}", mediaId, format);
    }
    return Optional.ofNullable(url);
  }

  /**
   * Invalidate all cache levels for a media item.
   *
   * <p>
   * Note: This only invalidates local L1 cache. For cross-instance
   * invalidation, use {@link CacheInvalidationPublisher}.
   *
   * @param mediaId The media ID to invalidate
   */
  public void invalidateLocal(String mediaId) {
    l1CacheManager.invalidateAll(mediaId);
    hotkeyService.clearCounter(mediaId);
    log.debug("Local caches invalidated for mediaId={}", mediaId);
  }

  /**
   * Get L1 cache statistics for monitoring.
   *
   * @return Cache statistics as a formatted string
   */
  public String getL1Stats() {
    return l1CacheManager.getStats();
  }

  // ==================== Private Helper Methods ====================

  private Optional<Media> readMediaFromL2(String mediaId) {
    return l2CacheManager.getMedia(mediaId);
  }

  private void writeMediaToCache(String mediaId, Media media) {
    boolean isHotKey = hotkeyService.isHotKey(mediaId);
    l2CacheManager.putMedia(mediaId, media, isHotKey);
    l1CacheManager.putMedia(mediaId, media);
    log.debug("Cached media {} (hotKey={})", mediaId, isHotKey);
  }
}
