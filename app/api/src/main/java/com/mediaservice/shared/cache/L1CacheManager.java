package com.mediaservice.shared.cache;

import com.github.benmanes.caffeine.cache.Cache;
import com.mediaservice.common.model.Media;
import com.mediaservice.shared.config.properties.MultiLevelCacheProperties;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Component;

import java.util.Optional;

/**
 * Manager for L1 (Caffeine) local cache operations.
 *
 * <p>
 * This is the single owner of all L1 cache operations, ensuring consistent
 * behavior across the application. All L1 cache access should go through
 * this manager.
 *
 * <p>
 * L1 cache characteristics:
 * <ul>
 * <li>Per-instance (JVM memory)</li>
 * <li>Microsecond access time</li>
 * <li>Size-limited with LRU eviction</li>
 * <li>Short TTL (30s default) for consistency</li>
 * </ul>
 */
@Component
@Slf4j
public class L1CacheManager {
  private final Cache<String, Media> mediaCache;
  private final Cache<String, String> presignedUrlCache;
  private final MultiLevelCacheProperties cacheProperties;

  public L1CacheManager(
      Cache<String, Media> localMediaCache,
      Cache<String, String> localPresignedUrlCache,
      MultiLevelCacheProperties cacheProperties) {
    this.mediaCache = localMediaCache;
    this.presignedUrlCache = localPresignedUrlCache;
    this.cacheProperties = cacheProperties;
  }

  /**
   * Check if L1 cache is enabled.
   *
   * @return true if L1 cache is enabled
   */
  public boolean isEnabled() {
    return cacheProperties.isEnabled() && cacheProperties.getLocal().isEnabled();
  }

  // ==================== Media Cache Operations ====================

  /**
   * Get media from L1 cache.
   *
   * @param mediaId The media ID
   * @return The cached media, or empty if not present
   */
  public Optional<Media> getMedia(String mediaId) {
    if (!isEnabled()) {
      return Optional.empty();
    }
    String key = CacheKeyProvider.mediaKey(mediaId);
    Media cached = mediaCache.getIfPresent(key);
    if (cached != null) {
      log.debug("L1 cache hit for mediaId={}", mediaId);
      return Optional.of(cached);
    }
    return Optional.empty();
  }

  /**
   * Put media into L1 cache.
   *
   * @param mediaId The media ID
   * @param media   The media object to cache
   */
  public void putMedia(String mediaId, Media media) {
    if (!isEnabled() || media == null) {
      return;
    }
    String key = CacheKeyProvider.mediaKey(mediaId);
    mediaCache.put(key, media);
    log.debug("L1 cache put for mediaId={}", mediaId);
  }

  /**
   * Invalidate media from L1 cache.
   *
   * @param mediaId The media ID to invalidate
   */
  public void invalidateMedia(String mediaId) {
    String key = CacheKeyProvider.mediaKey(mediaId);
    mediaCache.invalidate(key);
    log.debug("L1 cache invalidated for mediaId={}", mediaId);
  }

  // ==================== Presigned URL Cache Operations ====================

  /**
   * Get presigned URL from L1 cache.
   *
   * @param mediaId The media ID
   * @param format  The output format
   * @return The cached URL, or empty if not present
   */
  public Optional<String> getPresignedUrl(String mediaId, String format) {
    if (!isEnabled()) {
      return Optional.empty();
    }
    String key = CacheKeyProvider.presignedUrlKey(mediaId, format);
    String cached = presignedUrlCache.getIfPresent(key);
    if (cached != null) {
      log.debug("L1 presigned URL cache hit for key={}", key);
      return Optional.of(cached);
    }
    return Optional.empty();
  }

  /**
   * Put presigned URL into L1 cache.
   *
   * @param mediaId The media ID
   * @param format  The output format
   * @param url     The presigned URL to cache
   */
  public void putPresignedUrl(String mediaId, String format, String url) {
    if (!isEnabled() || url == null) {
      return;
    }
    String key = CacheKeyProvider.presignedUrlKey(mediaId, format);
    presignedUrlCache.put(key, url);
    log.debug("L1 presigned URL cache put for key={}", key);
  }

  /**
   * Invalidate presigned URLs for all formats of a media item.
   *
   * @param mediaId The media ID to invalidate
   */
  public void invalidatePresignedUrls(String mediaId) {
    for (String key : CacheKeyProvider.allPresignedUrlKeys(mediaId)) {
      presignedUrlCache.invalidate(key);
    }
    log.debug("L1 presigned URL cache invalidated for mediaId={}", mediaId);
  }

  // ==================== Combined Operations ====================

  /**
   * Invalidate all L1 caches for a media item.
   *
   * <p>
   * This invalidates:
   * <ul>
   * <li>Media metadata cache</li>
   * <li>Presigned URL cache (all formats)</li>
   * </ul>
   *
   * @param mediaId The media ID to invalidate
   */
  public void invalidateAll(String mediaId) {
    invalidateMedia(mediaId);
    invalidatePresignedUrls(mediaId);
    log.debug("L1 all caches invalidated for mediaId={}", mediaId);
  }

  // ==================== Statistics ====================

  /**
   * Get L1 cache statistics for monitoring.
   *
   * @return Cache statistics as a formatted string
   */
  public String getStats() {
    if (!cacheProperties.getLocal().isRecordStats()) {
      return "Stats recording disabled";
    }
    var stats = mediaCache.stats();
    return String.format(
        "L1 Cache Stats - hitRate=%.2f%%, hitCount=%d, missCount=%d, evictionCount=%d, size=%d",
        stats.hitRate() * 100,
        stats.hitCount(),
        stats.missCount(),
        stats.evictionCount(),
        mediaCache.estimatedSize());
  }

  /**
   * Get estimated size of L1 media cache.
   *
   * @return Estimated number of entries in cache
   */
  public long getMediaCacheSize() {
    return mediaCache.estimatedSize();
  }

  /**
   * Get estimated size of L1 presigned URL cache.
   *
   * @return Estimated number of entries in cache
   */
  public long getPresignedUrlCacheSize() {
    return presignedUrlCache.estimatedSize();
  }
}
