package com.mediaservice.service;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.script.DefaultRedisScript;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.util.Collections;
import java.util.List;

/**
 * Redis-based distributed rate limiting service using sliding window algorithm.
 *
 * <p>
 * Provides distributed rate limiting that works across multiple API instances,
 * enabling horizontal scaling with consistent rate limit enforcement.
 *
 * <p>
 * Uses Redis INCR with EXPIRE for efficient atomic operations. The sliding
 * window is implemented per-minute with automatic key expiration.
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class RedisRateLimitService {
  private final StringRedisTemplate redisTemplate;
  private static final String RATE_LIMIT_KEY_PREFIX = "rate_limit:";

  /**
   * Lua script for atomic rate limit check and increment.
   * Returns: [allowed (0/1), remaining tokens, ttl in seconds]
   */
  private static final String RATE_LIMIT_SCRIPT = """
      local key = KEYS[1]
      local limit = tonumber(ARGV[1])
      local window = tonumber(ARGV[2])

      local current = redis.call('INCR', key)

      if current == 1 then
        redis.call('EXPIRE', key, window)
      end

      local ttl = redis.call('TTL', key)

      if current > limit then
        return {0, 0, ttl}
      end

      return {1, limit - current, ttl}
      """;

  private final DefaultRedisScript<List> rateLimitScript = new DefaultRedisScript<>(RATE_LIMIT_SCRIPT, List.class);

  /**
   * Attempt to consume a token from the rate limit bucket.
   *
   * @param clientIp   Client IP address
   * @param bucketType Type of bucket (api or upload)
   * @param limit      Maximum requests allowed in the window
   * @param window     Time window duration
   * @return RateLimitResult containing allowed status and remaining tokens
   */
  public RateLimitResult tryConsume(String clientIp, String bucketType, int limit, Duration window) {
    String key = buildKey(clientIp, bucketType);
    long windowSeconds = window.toSeconds();

    try {
      @SuppressWarnings("unchecked")
      List<Long> result = redisTemplate.execute(
          rateLimitScript,
          Collections.singletonList(key),
          String.valueOf(limit),
          String.valueOf(windowSeconds));

      if (result == null || result.size() < 3) {
        log.warn("Unexpected rate limit script result for key: {}", key);
        return new RateLimitResult(true, limit, windowSeconds);
      }

      boolean allowed = result.get(0) == 1L;
      long remaining = result.get(1);
      long ttl = result.get(2);

      if (!allowed) {
        log.debug("Rate limit exceeded for key: {}, ttl: {}s", key, ttl);
      }

      return new RateLimitResult(allowed, remaining, ttl);
    } catch (Exception e) {
      log.error("Redis rate limit check failed for key: {}, allowing request: {}", key, e.getMessage());
      // Fail open - allow request if Redis is unavailable
      return new RateLimitResult(true, limit, windowSeconds);
    }
  }

  /**
   * Get current rate limit status without consuming a token.
   *
   * @param clientIp   Client IP address
   * @param bucketType Type of bucket (api or upload)
   * @param limit      Maximum requests allowed in the window
   * @return Current count of requests made
   */
  public long getCurrentCount(String clientIp, String bucketType, int limit) {
    String key = buildKey(clientIp, bucketType);
    try {
      String value = redisTemplate.opsForValue().get(key);
      if (value == null) {
        return 0;
      }
      long current = Long.parseLong(value);
      return Math.max(0, limit - current);
    } catch (Exception e) {
      log.error("Failed to get rate limit count for key: {}: {}", key, e.getMessage());
      return limit;
    }
  }

  private String buildKey(String clientIp, String bucketType) {
    return RATE_LIMIT_KEY_PREFIX + bucketType + ":" + clientIp;
  }

  /**
   * Result of a rate limit check.
   *
   * @param allowed           Whether the request is allowed
   * @param remaining         Number of remaining tokens
   * @param retryAfterSeconds Seconds until the window resets (for retry-after
   *                          header)
   */
  public record RateLimitResult(boolean allowed, long remaining, long retryAfterSeconds) {
  }
}
