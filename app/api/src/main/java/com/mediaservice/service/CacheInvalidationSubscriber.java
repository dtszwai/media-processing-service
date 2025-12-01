package com.mediaservice.service;

import com.mediaservice.service.cache.L1CacheManager;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

/**
 * Subscriber for Redis Pub/Sub cache invalidation messages.
 *
 * <p>
 * Listens for cache invalidation events published by other API instances
 * and evicts the corresponding entries from the local L1 (Caffeine) cache.
 *
 * <p>
 * This enables near-real-time cache consistency across all API instances,
 * reducing the staleness window from 30 seconds (L1 TTL) to typically &lt; 1ms.
 *
 * <p>
 * Message format: {@code mediaId} (plain string)
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class CacheInvalidationSubscriber {
  private final L1CacheManager l1CacheManager;

  /**
   * Handle cache invalidation message from Redis Pub/Sub.
   *
   * <p>
   * Called by Spring's MessageListenerAdapter when a message is received
   * on the cache invalidation channel.
   *
   * @param message The media ID to invalidate (received as String)
   * @param channel The Redis channel (unused but required by adapter)
   */
  public void onMessage(String message, String channel) {
    if (message == null || message.isBlank()) {
      log.debug("Received empty cache invalidation message, ignoring");
      return;
    }

    String mediaId = message.trim();

    try {
      // Delegate to L1CacheManager for all invalidation
      l1CacheManager.invalidateAll(mediaId);
      log.debug("L1 cache invalidated via Pub/Sub for mediaId={}", mediaId);
    } catch (Exception e) {
      log.warn("Failed to invalidate L1 cache via Pub/Sub for mediaId={}: {}", mediaId, e.getMessage());
    }
  }
}
