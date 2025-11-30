package com.mediaservice.service;

import com.mediaservice.config.RedisConfig;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.cache.CacheManager;
import org.springframework.stereotype.Service;

/**
 * Centralized service for cache invalidation.
 *
 * <p>
 * Provides methods to invalidate various caches when data changes,
 * ensuring consistency between cache and database.
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class CacheInvalidationService {
  private final CacheManager cacheManager;

  /**
   * Invalidate all caches related to a specific media item.
   *
   * @param mediaId The media ID to invalidate caches for
   */
  public void invalidateMedia(String mediaId) {
    evictFromCache(RedisConfig.CACHE_MEDIA, mediaId);
    evictFromCache(RedisConfig.CACHE_MEDIA_STATUS, mediaId);
    invalidatePresignedUrls(mediaId);
    invalidatePaginationCache();
    log.debug("Invalidated all caches for mediaId: {}", mediaId);
  }

  /**
   * Invalidate media status cache only.
   *
   * @param mediaId The media ID to invalidate status cache for
   */
  public void invalidateMediaStatus(String mediaId) {
    evictFromCache(RedisConfig.CACHE_MEDIA_STATUS, mediaId);
    log.debug("Invalidated status cache for mediaId: {}", mediaId);
  }

  /**
   * Invalidate presigned URL caches for all formats of a media item.
   *
   * @param mediaId The media ID to invalidate presigned URL caches for
   */
  public void invalidatePresignedUrls(String mediaId) {
    // Evict all format variants
    String[] formats = { "JPEG", "PNG", "WEBP" };
    for (String format : formats) {
      evictFromCache(RedisConfig.CACHE_PRESIGNED_URL, mediaId + ":" + format);
    }
    log.debug("Invalidated presigned URL caches for mediaId: {}", mediaId);
  }

  /**
   * Invalidate pagination cache.
   * Called when media items are added or removed.
   */
  public void invalidatePaginationCache() {
    var cache = cacheManager.getCache(RedisConfig.CACHE_PAGINATION);
    if (cache != null) {
      cache.clear();
      log.debug("Cleared pagination cache");
    }
  }

  private void evictFromCache(String cacheName, String key) {
    try {
      var cache = cacheManager.getCache(cacheName);
      if (cache != null) {
        cache.evict(key);
      }
    } catch (Exception e) {
      log.warn("Failed to evict cache {}:{}: {}", cacheName, key, e.getMessage());
    }
  }
}
