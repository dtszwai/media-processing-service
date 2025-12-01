package com.mediaservice.shared.cache;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

/**
 * Centralized service for cache invalidation.
 *
 * <p>
 * Provides methods to invalidate various caches when data changes,
 * ensuring consistency between cache and database.
 *
 * <p>
 * Invalidates:
 * <ul>
 * <li>L1 (Caffeine local cache) via L1CacheManager</li>
 * <li>L2 (Redis distributed cache) via L2CacheManager</li>
 * <li>Cross-instance L1 via Redis Pub/Sub</li>
 * </ul>
 *
 * <p>
 * Cross-instance L1 invalidation is achieved via Redis Pub/Sub:
 * when this instance invalidates a cache entry, it publishes a message
 * to Redis, and all other instances receive the message and evict their
 * local L1 caches.
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class CacheInvalidationService {
  private final L1CacheManager l1CacheManager;
  private final L2CacheManager l2CacheManager;
  private final CacheInvalidationPublisher invalidationPublisher;
  private final HotkeyDetectionService hotkeyDetectionService;

  /**
   * Invalidate all caches related to a specific media item.
   *
   * <p>
   * Invalidates both L1 (local Caffeine) and L2 (Redis) caches.
   * Also publishes invalidation event via Redis Pub/Sub for cross-instance
   * L1 cache invalidation.
   *
   * @param mediaId The media ID to invalidate caches for
   */
  public void invalidateMedia(String mediaId) {
    // L1: Invalidate local cache (this instance)
    l1CacheManager.invalidateAll(mediaId);

    // L1: Publish to Redis Pub/Sub for other instances
    invalidationPublisher.publish(mediaId);

    // L2: Invalidate Redis caches
    l2CacheManager.invalidateAll(mediaId);

    // Clear hotkey counter
    hotkeyDetectionService.clearCounter(mediaId);

    log.debug("Invalidated all caches (L1+L2+Pub/Sub) for mediaId={}", mediaId);
  }

  /**
   * Invalidate media status cache only.
   *
   * @param mediaId The media ID to invalidate status cache for
   */
  public void invalidateMediaStatus(String mediaId) {
    l2CacheManager.invalidateMediaStatus(mediaId);
    log.debug("Invalidated status cache for mediaId={}", mediaId);
  }

  /**
   * Invalidate presigned URL caches for all formats of a media item.
   *
   * @param mediaId The media ID to invalidate presigned URL caches for
   */
  public void invalidatePresignedUrls(String mediaId) {
    l1CacheManager.invalidatePresignedUrls(mediaId);
    l2CacheManager.invalidatePresignedUrls(mediaId);
    log.debug("Invalidated presigned URL caches for mediaId={}", mediaId);
  }

  /**
   * Invalidate pagination cache.
   * Called when media items are added or removed.
   */
  public void invalidatePaginationCache() {
    l2CacheManager.clearPaginationCache();
    log.debug("Cleared pagination cache");
  }
}
