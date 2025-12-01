package com.mediaservice.shared.cache.config;

import lombok.Getter;
import lombok.Setter;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.context.annotation.Configuration;

import java.time.Duration;

/**
 * Configuration for distributed rate limiting.
 *
 * <p>
 * Defines rate limit thresholds for different endpoint types:
 * <ul>
 * <li>API - General API requests (higher limits)</li>
 * <li>Upload - File upload endpoints (lower limits)</li>
 * </ul>
 *
 * <p>
 * Rate limiting is implemented using Redis for distributed enforcement
 * across multiple API instances.
 */
@Configuration
@ConfigurationProperties(prefix = "rate-limiting")
@Getter
@Setter
public class RateLimitingConfig {
  private boolean enabled = true;

  /**
   * Configuration for general API endpoints.
   */
  private Api api = new Api();

  /**
   * Configuration for upload endpoints.
   */
  private Upload upload = new Upload();

  /**
   * Time window for rate limiting.
   */
  private Duration window = Duration.ofMinutes(1);

  @Getter
  @Setter
  public static class Api {
    /**
     * Maximum requests per minute for general API calls.
     */
    private int requestsPerMinute = 100;

    /**
     * Burst capacity (not used with Redis sliding window, kept for compatibility).
     */
    private int burstCapacity = 20;
  }

  @Getter
  @Setter
  public static class Upload {
    /**
     * Maximum requests per minute for upload endpoints.
     */
    private int requestsPerMinute = 10;

    /**
     * Burst capacity (not used with Redis sliding window, kept for compatibility).
     */
    private int burstCapacity = 5;
  }
}
