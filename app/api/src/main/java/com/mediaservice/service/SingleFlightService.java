package com.mediaservice.service;

import com.mediaservice.config.MultiLevelCacheProperties;
import com.mediaservice.exception.CacheLoadTimeoutException;
import com.mediaservice.service.cache.CacheKeyProvider;
import io.github.resilience4j.circuitbreaker.annotation.CircuitBreaker;
import lombok.extern.slf4j.Slf4j;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.util.Optional;
import java.util.concurrent.*;
import java.util.function.Function;
import java.util.function.Supplier;

/**
 * Service implementing single-flight (singleflight) pattern for cache loading.
 *
 * <p>
 * Single-flight prevents the "thundering herd" problem where multiple
 * concurrent requests for the same uncached key all trigger database queries.
 * Instead, only one request loads from the database while others wait for
 * the cache to be populated.
 *
 * <p>
 * This is critical for multi-instance deployments where cache misses
 * can cause N instances × M concurrent requests = N×M database queries
 * for the same data.
 *
 * <p>
 * Implementation uses Redis distributed locks to coordinate across instances:
 *
 * <pre>
 * Instance 1: Cache miss → Acquire lock → Load from DB → Write cache → Release lock
 * Instance 2: Cache miss → Lock exists → Wait → Read from cache
 * Instance 3: Cache miss → Lock exists → Wait → Read from cache
 * </pre>
 *
 * <p>
 * This implementation uses async polling to avoid blocking threads,
 * making it suitable for high-concurrency scenarios.
 */
@Service
@Slf4j
public class SingleFlightService {
  private static final String CIRCUIT_BREAKER_NAME = "redis";

  private final StringRedisTemplate stringRedisTemplate;
  private final MultiLevelCacheProperties cacheProperties;
  private final ScheduledExecutorService scheduler;

  public SingleFlightService(
      StringRedisTemplate stringRedisTemplate,
      MultiLevelCacheProperties cacheProperties) {
    this.stringRedisTemplate = stringRedisTemplate;
    this.cacheProperties = cacheProperties;
    // Use virtual threads if available (Java 21+), otherwise use a small thread pool
    this.scheduler = Executors.newScheduledThreadPool(
        4,
        Thread.ofVirtual().name("single-flight-", 0).factory());
  }

  /**
   * Execute a cache load operation with single-flight protection.
   *
   * <p>
   * If this instance wins the lock, it executes the loader and caches the result.
   * If another instance holds the lock, this instance waits and reads from cache.
   *
   * @param key         The cache key
   * @param loader      Function to load data from the source (database)
   * @param cacheReader Function to read from cache
   * @param cacheWriter Function to write to cache
   * @param <T>         The type of cached data
   * @return The loaded or cached data, or empty if not found
   */
  @CircuitBreaker(name = CIRCUIT_BREAKER_NAME, fallbackMethod = "executeFallback")
  public <T> Optional<T> execute(
      String key,
      Supplier<Optional<T>> loader,
      Function<String, Optional<T>> cacheReader,
      java.util.function.BiConsumer<String, T> cacheWriter) {

    if (!cacheProperties.isEnabled() || !cacheProperties.getSingleFlight().isEnabled()) {
      // Single-flight disabled - load directly
      return loader.get();
    }

    var config = cacheProperties.getSingleFlight();
    String lockKey = CacheKeyProvider.singleFlightLockKey(key);

    // Try to acquire the lock
    Boolean acquired = stringRedisTemplate.opsForValue()
        .setIfAbsent(lockKey, "1", Duration.ofMillis(config.getLockTtlMs()));

    if (Boolean.TRUE.equals(acquired)) {
      // We won the lock - we are responsible for loading
      log.debug("Single-flight: acquired lock for key={}", key);
      try {
        Optional<T> result = loader.get();
        result.ifPresent(value -> cacheWriter.accept(key, value));
        return result;
      } finally {
        // Release lock
        stringRedisTemplate.delete(lockKey);
        log.debug("Single-flight: released lock for key={}", key);
      }
    } else {
      // Another instance is loading - wait for cache using async polling
      log.debug("Single-flight: waiting for cache population for key={}", key);
      return waitForCacheAsync(key, cacheReader, config.getMaxRetries(), config.getRetryIntervalMs());
    }
  }

  @SuppressWarnings("unused")
  private <T> Optional<T> executeFallback(
      String key,
      Supplier<Optional<T>> loader,
      Function<String, Optional<T>> cacheReader,
      java.util.function.BiConsumer<String, T> cacheWriter,
      Throwable t) {
    log.warn("Single-flight circuit breaker open, loading directly: {}", t.getMessage());
    return loader.get();
  }

  /**
   * Wait for cache to be populated by another instance using async polling.
   *
   * <p>
   * This method uses a CompletableFuture with scheduled retries to avoid
   * blocking the calling thread. However, the caller still gets a synchronous
   * result by calling get() on the future with a timeout.
   *
   * @param key           The cache key to wait for
   * @param cacheReader   Function to read from cache
   * @param maxRetries    Maximum number of retries
   * @param retryInterval Interval between retries in milliseconds
   * @param <T>           The type of cached data
   * @return The cached data, or throws CacheLoadTimeoutException on timeout
   */
  private <T> Optional<T> waitForCacheAsync(
      String key,
      Function<String, Optional<T>> cacheReader,
      int maxRetries,
      long retryInterval) {

    CompletableFuture<Optional<T>> future = new CompletableFuture<>();
    pollForCache(key, cacheReader, future, 0, maxRetries, retryInterval);

    try {
      // Total timeout = maxRetries * retryInterval + buffer
      long timeoutMs = (maxRetries * retryInterval) + 1000;
      return future.get(timeoutMs, TimeUnit.MILLISECONDS);
    } catch (TimeoutException e) {
      log.warn("Single-flight: async timeout waiting for cache key={}", key);
      throw new CacheLoadTimeoutException("Timeout waiting for cache population: " + key);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new CacheLoadTimeoutException("Interrupted while waiting for cache: " + key, e);
    } catch (ExecutionException e) {
      if (e.getCause() instanceof CacheLoadTimeoutException) {
        throw (CacheLoadTimeoutException) e.getCause();
      }
      throw new CacheLoadTimeoutException("Error waiting for cache: " + key, e.getCause());
    }
  }

  /**
   * Recursively poll for cache with scheduled delays.
   */
  private <T> void pollForCache(
      String key,
      Function<String, Optional<T>> cacheReader,
      CompletableFuture<Optional<T>> future,
      int attempt,
      int maxRetries,
      long retryInterval) {

    if (attempt >= maxRetries) {
      future.completeExceptionally(
          new CacheLoadTimeoutException("Max retries exceeded waiting for cache: " + key));
      return;
    }

    // Check cache
    Optional<T> cached = cacheReader.apply(key);
    if (cached.isPresent()) {
      log.debug("Single-flight: cache populated after {} attempts for key={}", attempt, key);
      future.complete(cached);
      return;
    }

    // Check if lock is still held
    String lockKey = CacheKeyProvider.singleFlightLockKey(key);
    Boolean lockExists = stringRedisTemplate.hasKey(lockKey);

    if (Boolean.FALSE.equals(lockExists)) {
      // Lock released but cache empty - data might not exist
      Optional<T> finalCheck = cacheReader.apply(key);
      if (finalCheck.isPresent()) {
        future.complete(finalCheck);
      } else {
        log.debug("Single-flight: lock released but cache empty for key={}, data may not exist", key);
        future.complete(Optional.empty());
      }
      return;
    }

    // Schedule next poll
    scheduler.schedule(
        () -> pollForCache(key, cacheReader, future, attempt + 1, maxRetries, retryInterval),
        retryInterval,
        TimeUnit.MILLISECONDS);
  }

  /**
   * Check if a load is currently in progress for a key.
   *
   * @param key The cache key
   * @return true if another instance is currently loading this key
   */
  public boolean isLoadInProgress(String key) {
    if (!cacheProperties.isEnabled() || !cacheProperties.getSingleFlight().isEnabled()) {
      return false;
    }
    String lockKey = CacheKeyProvider.singleFlightLockKey(key);
    return Boolean.TRUE.equals(stringRedisTemplate.hasKey(lockKey));
  }
}
