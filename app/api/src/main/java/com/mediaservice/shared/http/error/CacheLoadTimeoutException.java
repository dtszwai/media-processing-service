package com.mediaservice.shared.http.error;

/**
 * Exception thrown when waiting for cache to be populated times out.
 *
 * <p>
 * This occurs in single-flight scenarios when:
 * <ul>
 * <li>Another instance is loading data from the database</li>
 * <li>This instance is waiting for the cache to be populated</li>
 * <li>The wait exceeds the configured timeout</li>
 * </ul>
 */
public class CacheLoadTimeoutException extends RuntimeException {
  public CacheLoadTimeoutException(String message) {
    super(message);
  }

  public CacheLoadTimeoutException(String message, Throwable cause) {
    super(message, cause);
  }
}
