package com.mediaservice.config;

import lombok.Getter;
import lombok.Setter;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

/**
 * Configuration properties for cache TTL settings.
 *
 * <p>
 * Provides externalized configuration for cache time-to-live values,
 * allowing different TTLs for different cache types.
 */
@Component
@ConfigurationProperties(prefix = "cache")
@Getter
@Setter
public class CacheProperties {
  private CacheTtl media = new CacheTtl(300);
  private CacheTtl status = new CacheTtl(30);
  private CacheTtl presignedUrl = new CacheTtl(3500);
  private CacheTtl pagination = new CacheTtl(120);

  @Getter
  @Setter
  public static class CacheTtl {
    private int ttlSeconds;

    public CacheTtl() {
    }

    public CacheTtl(int ttlSeconds) {
      this.ttlSeconds = ttlSeconds;
    }
  }
}
