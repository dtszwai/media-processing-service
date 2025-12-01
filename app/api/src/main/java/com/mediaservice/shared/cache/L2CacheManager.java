package com.mediaservice.shared.cache;

import com.mediaservice.common.model.Media;
import com.mediaservice.shared.config.properties.CacheProperties;
import com.mediaservice.shared.config.properties.MultiLevelCacheProperties;

import io.github.resilience4j.circuitbreaker.annotation.CircuitBreaker;
import lombok.extern.slf4j.Slf4j;
import org.springframework.cache.Cache;
import org.springframework.cache.CacheManager;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.stereotype.Component;

import java.time.Duration;
import java.util.Optional;
import java.util.concurrent.TimeUnit;

/**
 * Manager for L2 (Redis) distributed cache operations.
 *
 * <p>
 * This is the single owner of all L2 cache operations, ensuring consistent
 * behavior and key handling across the application.
 *
 * <p>
 * L2 cache characteristics:
 * <ul>
 * <li>Distributed (shared across all API instances)</li>
 * <li>Millisecond access time</li>
 * <li>TTL-based expiration with jitter</li>
 * <li>Supports hotkey TTL extension</li>
 * </ul>
 *
 * <p>
 * Uses circuit breaker to prevent cascading failures when Redis is slow/down.
 */
@Component
@Slf4j
public class L2CacheManager {
  private static final String CIRCUIT_BREAKER_NAME = "redis";

  private final CacheManager cacheManager;
  private final RedisTemplate<String, Object> redisTemplate;
  private final CacheProperties cacheProperties;
  private final MultiLevelCacheProperties multiLevelCacheProperties;

  public L2CacheManager(
      CacheManager cacheManager,
      RedisTemplate<String, Object> redisTemplate,
      CacheProperties cacheProperties,
      MultiLevelCacheProperties multiLevelCacheProperties) {
    this.cacheManager = cacheManager;
    this.redisTemplate = redisTemplate;
    this.cacheProperties = cacheProperties;
    this.multiLevelCacheProperties = multiLevelCacheProperties;
  }

  // ==================== Media Cache Operations ====================

  /**
   * Get media from L2 cache.
   *
   * @param mediaId The media ID
   * @return The cached media, or empty if not present
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "getMediaFallback")
  public Optional<Media> getMedia(String mediaId) {
    String key = CacheKeyProvider.mediaKey(mediaId);
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_MEDIA);
    if (cache != null) {
      Media cached = cache.get(key, Media.class);
      if (cached != null) {
        log.debug("L2 cache hit for mediaId={}", mediaId);
        return Optional.of(cached);
      }
    }
    return Optional.empty();
  }

  @SuppressWarnings("unused")
  private Optional<Media> getMediaFallback(String mediaId, Throwable t) {
    log.warn("L2 cache circuit breaker open for getMedia, mediaId={}: {}", mediaId, t.getMessage());
    return Optional.empty();
  }

  /**
   * Put media into L2 cache with TTL.
   *
   * @param mediaId  The media ID
   * @param media    The media object to cache
   * @param isHotKey Whether this is a hot key (extends TTL)
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "putMediaFallback")
  public void putMedia(String mediaId, Media media, boolean isHotKey) {
    if (media == null) {
      return;
    }
    Duration ttl = calculateTtl(
        Duration.ofSeconds(cacheProperties.getMedia().getTtlSeconds()),
        isHotKey);

    // Use RedisTemplate for custom TTL support
    String redisKey = CacheKeyProvider.CACHE_MEDIA + "::" + CacheKeyProvider.mediaKey(mediaId);
    redisTemplate.opsForValue().set(redisKey, media, ttl.toMillis(), TimeUnit.MILLISECONDS);
    log.debug("L2 cache put for mediaId={}, ttl={}, hotKey={}", mediaId, ttl, isHotKey);
  }

  @SuppressWarnings("unused")
  private void putMediaFallback(String mediaId, Media media, boolean isHotKey, Throwable t) {
    log.warn("L2 cache circuit breaker open for putMedia, mediaId={}: {}", mediaId, t.getMessage());
  }

  /**
   * Invalidate media from L2 cache.
   *
   * @param mediaId The media ID to invalidate
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "invalidateMediaFallback")
  public void invalidateMedia(String mediaId) {
    String key = CacheKeyProvider.mediaKey(mediaId);
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_MEDIA);
    if (cache != null) {
      cache.evict(key);
      log.debug("L2 cache invalidated for mediaId={}", mediaId);
    }
  }

  @SuppressWarnings("unused")
  private void invalidateMediaFallback(String mediaId, Throwable t) {
    log.warn("L2 cache circuit breaker open for invalidateMedia, mediaId={}: {}", mediaId, t.getMessage());
  }

  // ==================== Media Status Cache Operations ====================

  /**
   * Invalidate media status from L2 cache.
   *
   * @param mediaId The media ID to invalidate
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "invalidateMediaStatusFallback")
  public void invalidateMediaStatus(String mediaId) {
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_MEDIA_STATUS);
    if (cache != null) {
      cache.evict(mediaId);
      log.debug("L2 media status cache invalidated for mediaId={}", mediaId);
    }
  }

  @SuppressWarnings("unused")
  private void invalidateMediaStatusFallback(String mediaId, Throwable t) {
    log.warn("L2 cache circuit breaker open for invalidateMediaStatus: {}", t.getMessage());
  }

  // ==================== Presigned URL Cache Operations ====================

  /**
   * Get presigned URL from L2 cache.
   *
   * @param mediaId The media ID
   * @param format  The output format
   * @return The cached URL, or empty if not present
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "getPresignedUrlFallback")
  public Optional<String> getPresignedUrl(String mediaId, String format) {
    String key = CacheKeyProvider.presignedUrlKey(mediaId, format);
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_PRESIGNED_URL);
    if (cache != null) {
      String cached = cache.get(key, String.class);
      if (cached != null) {
        log.debug("L2 presigned URL cache hit for key={}", key);
        return Optional.of(cached);
      }
    }
    return Optional.empty();
  }

  @SuppressWarnings("unused")
  private Optional<String> getPresignedUrlFallback(String mediaId, String format, Throwable t) {
    log.warn("L2 cache circuit breaker open for getPresignedUrl: {}", t.getMessage());
    return Optional.empty();
  }

  /**
   * Put presigned URL into L2 cache.
   *
   * @param mediaId The media ID
   * @param format  The output format
   * @param url     The presigned URL to cache
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "putPresignedUrlFallback")
  public void putPresignedUrl(String mediaId, String format, String url) {
    if (url == null) {
      return;
    }
    String key = CacheKeyProvider.presignedUrlKey(mediaId, format);
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_PRESIGNED_URL);
    if (cache != null) {
      cache.put(key, url);
      log.debug("L2 presigned URL cache put for key={}", key);
    }
  }

  @SuppressWarnings("unused")
  private void putPresignedUrlFallback(String mediaId, String format, String url, Throwable t) {
    log.warn("L2 cache circuit breaker open for putPresignedUrl: {}", t.getMessage());
  }

  /**
   * Invalidate presigned URLs for all formats of a media item.
   *
   * @param mediaId The media ID to invalidate
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "invalidatePresignedUrlsFallback")
  public void invalidatePresignedUrls(String mediaId) {
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_PRESIGNED_URL);
    if (cache != null) {
      for (String key : CacheKeyProvider.allPresignedUrlKeys(mediaId)) {
        cache.evict(key);
      }
      log.debug("L2 presigned URL cache invalidated for mediaId={}", mediaId);
    }
  }

  @SuppressWarnings("unused")
  private void invalidatePresignedUrlsFallback(String mediaId, Throwable t) {
    log.warn("L2 cache circuit breaker open for invalidatePresignedUrls: {}", t.getMessage());
  }

  // ==================== Pagination Cache Operations ====================

  /**
   * Clear the entire pagination cache.
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "clearPaginationCacheFallback")
  public void clearPaginationCache() {
    Cache cache = cacheManager.getCache(CacheKeyProvider.CACHE_PAGINATION);
    if (cache != null) {
      cache.clear();
      log.debug("L2 pagination cache cleared");
    }
  }

  @SuppressWarnings("unused")
  private void clearPaginationCacheFallback(Throwable t) {
    log.warn("L2 cache circuit breaker open for clearPaginationCache: {}", t.getMessage());
  }

  // ==================== Combined Operations ====================

  /**
   * Invalidate all L2 caches for a media item.
   *
   * @param mediaId The media ID to invalidate
   */
  public void invalidateAll(String mediaId) {
    invalidateMedia(mediaId);
    invalidateMediaStatus(mediaId);
    invalidatePresignedUrls(mediaId);
    clearPaginationCache();
  }

  // ==================== TTL Calculation ====================

  /**
   * Calculate TTL with hotkey extension and jitter.
   *
   * @param baseTtl  Base TTL duration
   * @param isHotKey Whether this is a hot key
   * @return Calculated TTL with extensions and jitter applied
   */
  private Duration calculateTtl(Duration baseTtl, boolean isHotKey) {
    Duration ttl = baseTtl;

    // Apply hotkey multiplier
    if (isHotKey && multiLevelCacheProperties.getHotkey().isEnabled()) {
      int multiplier = multiLevelCacheProperties.getHotkey().getTtlMultiplier();
      ttl = ttl.multipliedBy(multiplier);
    }

    // Apply jitter
    if (multiLevelCacheProperties.getJitter().isEnabled()) {
      ttl = CacheTtlUtils.withJitter(ttl, multiLevelCacheProperties.getJitter().getMaxPercent());
    }

    return ttl;
  }
}
