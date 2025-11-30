package com.mediaservice.config;

import io.github.bucket4j.Bandwidth;
import io.github.bucket4j.Bucket;
import lombok.Getter;
import lombok.Setter;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import com.github.benmanes.caffeine.cache.Cache;
import com.github.benmanes.caffeine.cache.Caffeine;

import java.time.Duration;

@Configuration
@ConfigurationProperties(prefix = "rate-limiting")
@Getter
@Setter
public class RateLimitingConfig {
  private boolean enabled = true;
  private Api api = new Api();
  private Upload upload = new Upload();

  @Getter
  @Setter
  public static class Api {
    private int requestsPerMinute = 100;
    private int burstCapacity = 20;
  }

  @Getter
  @Setter
  public static class Upload {
    private int requestsPerMinute = 10;
    private int burstCapacity = 5;
  }

  @Bean
  public Cache<String, Bucket> apiRateLimitCache() {
    return Caffeine.newBuilder()
        .maximumSize(100_000)
        .expireAfterAccess(Duration.ofMinutes(10))
        .build();
  }

  @Bean
  public Cache<String, Bucket> uploadRateLimitCache() {
    return Caffeine.newBuilder()
        .maximumSize(100_000)
        .expireAfterAccess(Duration.ofMinutes(10))
        .build();
  }

  public Bucket createApiBucket() {
    return Bucket.builder()
        .addLimit(Bandwidth.builder()
            .capacity(api.getBurstCapacity())
            .refillGreedy(api.getRequestsPerMinute(), Duration.ofMinutes(1))
            .build())
        .build();
  }

  public Bucket createUploadBucket() {
    return Bucket.builder()
        .addLimit(Bandwidth.builder()
            .capacity(upload.getBurstCapacity())
            .refillGreedy(upload.getRequestsPerMinute(), Duration.ofMinutes(1))
            .build())
        .build();
  }
}
