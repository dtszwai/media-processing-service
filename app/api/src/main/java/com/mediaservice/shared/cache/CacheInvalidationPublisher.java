package com.mediaservice.shared.cache;

import io.github.resilience4j.circuitbreaker.annotation.CircuitBreaker;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.stereotype.Component;

/**
 * Publisher for cross-instance cache invalidation via Redis Pub/Sub.
 *
 * <p>
 * When cache is invalidated on one API instance, this publisher sends
 * a message to Redis Pub/Sub channel. All other instances subscribe to
 * this channel and invalidate their local L1 caches accordingly.
 *
 * <p>
 * This ensures cache consistency across all API instances with minimal
 * latency (typically &lt; 1ms for Redis Pub/Sub).
 *
 * <p>
 * Failure handling:
 * <ul>
 * <li>If Pub/Sub fails, other instances will still expire via TTL</li>
 * <li>Circuit breaker prevents cascading failures</li>
 * </ul>
 */
@Component
@RequiredArgsConstructor
@Slf4j
public class CacheInvalidationPublisher {
  private static final String CIRCUIT_BREAKER_NAME = "redis";

  private final StringRedisTemplate stringRedisTemplate;

  /**
   * Publish cache invalidation event for a media item.
   *
   * <p>
   * All API instances subscribed to the invalidation channel will
   * receive this message and evict their L1 caches.
   *
   * @param mediaId The media ID to invalidate across all instances
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "publishFallback")
  public void publish(String mediaId) {
    if (mediaId == null || mediaId.isBlank()) {
      log.debug("Skipping publish for null/empty mediaId");
      return;
    }

    try {
      stringRedisTemplate.convertAndSend(CacheKeyProvider.CACHE_INVALIDATION_CHANNEL, mediaId);
      log.debug("Published cache invalidation event for mediaId={}", mediaId);
    } catch (Exception e) {
      // Log at warn level - this is non-critical as TTL will eventually expire
      log.warn("Failed to publish cache invalidation for {}: {}", mediaId, e.getMessage());
    }
  }

  /**
   * Fallback when circuit breaker is open.
   *
   * <p>
   * When Redis Pub/Sub is unavailable, we log and continue.
   * Other instances will eventually expire their caches via TTL.
   */
  @SuppressWarnings("unused")
  private void publishFallback(String mediaId, Throwable t) {
    log.warn("Cache invalidation Pub/Sub circuit breaker open for mediaId={}: {}",
        mediaId, t.getMessage());
    // Non-critical: other instances will expire via TTL
  }
}
