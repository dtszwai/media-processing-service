package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.LambdaConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import redis.clients.jedis.JedisPool;
import redis.clients.jedis.JedisPoolConfig;
import redis.clients.jedis.resps.Tuple;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import java.util.stream.Collectors;

/**
 * Redis client service for Lambda analytics operations.
 * Uses Jedis connection pool for efficient Redis access.
 */
public class RedisService {
  private static final Logger logger = LoggerFactory.getLogger(RedisService.class);

  private final JedisPool jedisPool;

  public RedisService() {
    var config = LambdaConfig.getInstance();
    var poolConfig = new JedisPoolConfig();
    poolConfig.setMaxTotal(10);
    poolConfig.setMaxIdle(5);
    poolConfig.setMinIdle(1);
    poolConfig.setTestOnBorrow(true);
    this.jedisPool = new JedisPool(poolConfig, config.getRedisHost(), config.getRedisPort());
    logger.info("RedisService initialized with host: {}, port: {}", config.getRedisHost(), config.getRedisPort());
  }

  /**
   * Constructor for testing with custom JedisPool.
   */
  RedisService(JedisPool jedisPool) {
    this.jedisPool = jedisPool;
  }

  /**
   * Get all members of a sorted set with their scores.
   *
   * @param key Redis key for the sorted set
   * @return Map of member to score (ordered by score descending)
   */
  public Map<String, Long> getZSetWithScores(String key) {
    try (var jedis = jedisPool.getResource()) {
      var entries = jedis.zrangeWithScores(key, 0, -1);
      if (entries == null || entries.isEmpty()) {
        return Collections.emptyMap();
      }
      // Convert to map, sorted by score descending
      return entries.stream()
          .sorted((a, b) -> Double.compare(b.getScore(), a.getScore()))
          .collect(Collectors.toMap(
              Tuple::getElement,
              t -> (long) t.getScore(),
              (existing, replacement) -> existing,
              LinkedHashMap::new));
    } catch (Exception e) {
      logger.error("Failed to get ZSet with scores for key {}: {}", key, e.getMessage());
      return Collections.emptyMap();
    }
  }

  /**
   * Get all keys matching a pattern.
   *
   * @param pattern Redis key pattern (e.g., "views:daily:*")
   * @return Set of matching keys
   */
  public Set<String> getKeys(String pattern) {
    try (var jedis = jedisPool.getResource()) {
      return jedis.keys(pattern);
    } catch (Exception e) {
      logger.error("Failed to get keys for pattern {}: {}", pattern, e.getMessage());
      return Collections.emptySet();
    }
  }

  /**
   * Get a string value from Redis.
   *
   * @param key Redis key
   * @return Value or null if not found
   */
  public String getValue(String key) {
    try (var jedis = jedisPool.getResource()) {
      return jedis.get(key);
    } catch (Exception e) {
      logger.error("Failed to get value for key {}: {}", key, e.getMessage());
      return null;
    }
  }

  /**
   * Get all fields and values from a hash.
   *
   * @param key Redis hash key
   * @return Map of field to value
   */
  public Map<String, String> getHashAll(String key) {
    try (var jedis = jedisPool.getResource()) {
      return jedis.hgetAll(key);
    } catch (Exception e) {
      logger.error("Failed to get hash for key {}: {}", key, e.getMessage());
      return Collections.emptyMap();
    }
  }

  /**
   * Check if Redis connection is healthy.
   *
   * @return true if connection is healthy
   */
  public boolean isHealthy() {
    try (var jedis = jedisPool.getResource()) {
      return "PONG".equals(jedis.ping());
    } catch (Exception e) {
      logger.error("Redis health check failed: {}", e.getMessage());
      return false;
    }
  }

  /**
   * Close the connection pool.
   * Should be called when the Lambda is being shut down.
   */
  public void close() {
    if (jedisPool != null && !jedisPool.isClosed()) {
      jedisPool.close();
      logger.info("Redis connection pool closed");
    }
  }
}
