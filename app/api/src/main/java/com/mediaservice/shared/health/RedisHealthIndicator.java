package com.mediaservice.shared.health;

import lombok.extern.slf4j.Slf4j;
import org.springframework.boot.actuate.health.Health;
import org.springframework.boot.actuate.health.HealthIndicator;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.stereotype.Component;

import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Health indicator for Redis connectivity.
 *
 * <p>
 * Checks Redis availability via PING command. Results are cached
 * for 30 seconds to prevent excessive load (matches S3HealthIndicator pattern).
 */
@Component("redis")
@Slf4j
public class RedisHealthIndicator implements HealthIndicator {
  private final StringRedisTemplate redisTemplate;
  private final AtomicReference<CachedHealth> cachedHealth = new AtomicReference<>();
  private static final Duration CACHE_TTL = Duration.ofSeconds(30);

  public RedisHealthIndicator(StringRedisTemplate redisTemplate) {
    this.redisTemplate = redisTemplate;
  }

  @Override
  public Health health() {
    var cached = cachedHealth.get();
    if (cached != null && !cached.isExpired()) {
      return cached.health();
    }
    var health = checkHealth();
    cachedHealth.set(new CachedHealth(health, Instant.now().plus(CACHE_TTL)));
    return health;
  }

  private Health checkHealth() {
    try {
      var connectionFactory = redisTemplate.getConnectionFactory();
      if (connectionFactory == null) {
        return Health.down()
            .withDetail("reason", "Redis connection factory is null")
            .build();
      }
      // Execute PING command
      var result = redisTemplate.getConnectionFactory()
          .getConnection()
          .ping();
      return Health.up()
          .withDetail("redis", "available")
          .withDetail("response", result != null ? result : "PONG")
          .build();
    } catch (Exception e) {
      log.warn("Redis health check failed: {}", e.getMessage());
      return Health.down()
          .withDetail("error", e.getMessage())
          .build();
    }
  }

  private record CachedHealth(Health health, Instant expiresAt) {
    boolean isExpired() {
      return Instant.now().isAfter(expiresAt);
    }
  }
}
