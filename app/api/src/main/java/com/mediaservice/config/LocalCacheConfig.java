package com.mediaservice.config;

import com.github.benmanes.caffeine.cache.Cache;
import com.github.benmanes.caffeine.cache.Caffeine;
import com.mediaservice.common.model.Media;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.util.concurrent.TimeUnit;

/**
 * Configuration for L1 local cache using Caffeine.
 *
 * <p>
 * L1 cache provides microsecond-level access to frequently used data,
 * reducing Redis round-trips for hot data. Each API instance has its own
 * L1 cache, so data may be slightly stale compared to Redis.
 *
 * <p>
 * Use cases:
 * <ul>
 * <li>Media metadata for hot content</li>
 * <li>Presigned URLs (short TTL)</li>
 * </ul>
 */
@Configuration
@RequiredArgsConstructor
@Slf4j
public class LocalCacheConfig {
  private final MultiLevelCacheProperties cacheProperties;

  /**
   * L1 cache for Media objects.
   *
   * <p>
   * Configuration:
   * <ul>
   * <li>Max size: Configurable, default 1000 entries</li>
   * <li>TTL: Configurable, default 30 seconds (shorter than Redis)</li>
   * <li>Eviction: Size-based LRU when max size exceeded</li>
   * <li>Stats: Optional recording for monitoring</li>
   * </ul>
   */
  @Bean
  public Cache<String, Media> localMediaCache() {
    var config = cacheProperties.getLocal();
    var builder = Caffeine.newBuilder()
        .maximumSize(config.getMaxSize())
        .expireAfterWrite(config.getTtlSeconds(), TimeUnit.SECONDS);
    if (config.isRecordStats()) {
      builder.recordStats();
    }
    log.info("L1 local cache configured: maxSize={}, ttlSeconds={}, recordStats={}",
        config.getMaxSize(), config.getTtlSeconds(), config.isRecordStats());
    return builder.build();
  }

  /**
   * L1 cache for presigned URLs.
   *
   * <p>
   * Separate cache for URLs with potentially different characteristics.
   */
  @Bean
  public Cache<String, String> localPresignedUrlCache() {
    var config = cacheProperties.getLocal();
    var builder = Caffeine.newBuilder()
        .maximumSize(config.getMaxSize())
        .expireAfterWrite(config.getTtlSeconds(), TimeUnit.SECONDS);
    if (config.isRecordStats()) {
      builder.recordStats();
    }
    return builder.build();
  }
}
