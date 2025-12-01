package com.mediaservice.shared.cache;

import java.time.Duration;
import java.util.concurrent.ThreadLocalRandom;

/**
 * Utility class for cache TTL calculations.
 *
 * <p>
 * Provides methods for adding jitter to TTL values to prevent
 * cache avalanche (thundering herd when many items expire simultaneously).
 */
public final class CacheTtlUtils {

  private CacheTtlUtils() {
    // Utility class - prevent instantiation
  }

  /**
   * Add random jitter to TTL to prevent cache avalanche.
   *
   * <p>
   * Cache avalanche occurs when many cached items expire at the same time,
   * causing a sudden surge of requests to the database. By adding random jitter,
   * expiration times are spread out, preventing this problem.
   *
   * <p>
   * Example with 10% jitter on 5 minute TTL:
   * <ul>
   * <li>Base TTL: 300 seconds</li>
   * <li>Jitter range: ±30 seconds</li>
   * <li>Result: 270-330 seconds</li>
   * </ul>
   *
   * @param baseTtl       The base TTL duration
   * @param jitterPercent Maximum jitter as a percentage (0.0 to 1.0). 0.1 = ±10%
   * @return TTL with random jitter applied
   */
  public static Duration withJitter(Duration baseTtl, double jitterPercent) {
    if (baseTtl == null || baseTtl.isZero() || baseTtl.isNegative()) {
      return baseTtl;
    }
    if (jitterPercent <= 0) {
      return baseTtl;
    }
    // Clamp jitter to reasonable bounds
    double effectiveJitter = Math.min(jitterPercent, 0.5);
    long baseMillis = baseTtl.toMillis();
    // Generate random value between -1.0 and 1.0 using ThreadLocalRandom (faster
    // than SecureRandom)
    double jitterFactor = (ThreadLocalRandom.current().nextDouble() * 2 - 1) * effectiveJitter;
    long jitterMillis = (long) (baseMillis * jitterFactor);
    return Duration.ofMillis(baseMillis + jitterMillis);
  }

  /**
   * Add default 10% jitter to TTL.
   *
   * @param baseTtl The base TTL duration
   * @return TTL with ±10% random jitter applied
   */
  public static Duration withJitter(Duration baseTtl) {
    return withJitter(baseTtl, 0.1);
  }

  /**
   * Calculate TTL in seconds with jitter.
   *
   * @param baseTtlSeconds Base TTL in seconds
   * @param jitterPercent  Maximum jitter as a percentage (0.0 to 1.0)
   * @return TTL in seconds with jitter applied
   */
  public static long withJitterSeconds(long baseTtlSeconds, double jitterPercent) {
    return withJitter(Duration.ofSeconds(baseTtlSeconds), jitterPercent).getSeconds();
  }

  /**
   * Extend TTL for hot keys.
   *
   * <p>
   * Hot keys (frequently accessed items) benefit from longer TTL to:
   * <ul>
   * <li>Reduce cache misses for popular content</li>
   * <li>Decrease database load</li>
   * <li>Improve response times</li>
   * </ul>
   *
   * @param baseTtl    Base TTL duration
   * @param multiplier TTL multiplier for hot keys (e.g., 3 = 3x longer TTL)
   * @return Extended TTL
   */
  public static Duration extendForHotKey(Duration baseTtl, int multiplier) {
    if (baseTtl == null || multiplier <= 1) {
      return baseTtl;
    }
    return baseTtl.multipliedBy(multiplier);
  }
}
