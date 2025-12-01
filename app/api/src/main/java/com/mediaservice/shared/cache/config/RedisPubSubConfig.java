package com.mediaservice.shared.cache.config;

import com.mediaservice.shared.cache.CacheInvalidationSubscriber;
import com.mediaservice.shared.cache.CacheKeyProvider;
import lombok.extern.slf4j.Slf4j;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.data.redis.connection.RedisConnectionFactory;
import org.springframework.data.redis.listener.ChannelTopic;
import org.springframework.data.redis.listener.RedisMessageListenerContainer;
import org.springframework.data.redis.listener.adapter.MessageListenerAdapter;

/**
 * Redis Pub/Sub configuration for cross-instance cache invalidation.
 *
 * <p>
 * When one API instance invalidates a cache entry, it publishes a message
 * to a Redis channel. All other instances subscribe to this channel and
 * invalidate their local L1 (Caffeine) caches accordingly.
 *
 * <p>
 * This ensures cache consistency across all API instances with minimal
 * latency (typically &lt; 1ms for Redis Pub/Sub).
 */
@Configuration
@Slf4j
public class RedisPubSubConfig {

  /**
   * Topic for cache invalidation channel.
   */
  @Bean
  public ChannelTopic cacheInvalidationTopic() {
    return new ChannelTopic(CacheKeyProvider.CACHE_INVALIDATION_CHANNEL);
  }

  /**
   * Message listener adapter that delegates to CacheInvalidationSubscriber.
   */
  @Bean
  public MessageListenerAdapter cacheInvalidationListenerAdapter(CacheInvalidationSubscriber subscriber) {
    return new MessageListenerAdapter(subscriber, "onMessage");
  }

  /**
   * Redis message listener container that manages subscriptions.
   *
   * <p>
   * Automatically subscribes to the cache invalidation channel on startup
   * and handles reconnection on Redis connection failures.
   */
  @Bean
  public RedisMessageListenerContainer redisMessageListenerContainer(
      RedisConnectionFactory connectionFactory,
      MessageListenerAdapter cacheInvalidationListenerAdapter,
      ChannelTopic cacheInvalidationTopic) {
    var container = new RedisMessageListenerContainer();
    container.setConnectionFactory(connectionFactory);
    container.addMessageListener(cacheInvalidationListenerAdapter, cacheInvalidationTopic);
    container.setErrorHandler(e -> log.warn("Error in Redis Pub/Sub listener: {}", e.getMessage()));
    log.info("Redis Pub/Sub configured for cache invalidation on channel: {}", CacheKeyProvider.CACHE_INVALIDATION_CHANNEL);
    return container;
  }
}
