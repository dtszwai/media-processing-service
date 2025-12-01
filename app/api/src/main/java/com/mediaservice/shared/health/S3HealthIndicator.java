package com.mediaservice.shared.health;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.actuate.health.Health;
import org.springframework.boot.actuate.health.HealthIndicator;
import org.springframework.stereotype.Component;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.HeadBucketRequest;

import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Health indicator for S3 bucket connectivity.
 *
 * <p>
 * Checks if the configured S3 bucket is accessible by performing a HEAD
 * request.
 * Results are cached for 30 seconds to prevent excessive load on S3.
 */
@Component
@Slf4j
public class S3HealthIndicator implements HealthIndicator {
  private final S3Client s3Client;
  private final AtomicReference<CachedHealth> cachedHealth = new AtomicReference<>();
  private static final Duration CACHE_TTL = Duration.ofSeconds(30);

  @Value("${aws.s3.bucket-name}")
  private String bucketName;

  public S3HealthIndicator(S3Client s3Client) {
    this.s3Client = s3Client;
  }

  @Override
  public Health health() {
    CachedHealth cached = cachedHealth.get();
    if (cached != null && !cached.isExpired()) {
      return cached.health();
    }

    Health health = checkHealth();
    cachedHealth.set(new CachedHealth(health, Instant.now().plus(CACHE_TTL)));
    return health;
  }

  private Health checkHealth() {
    try {
      s3Client.headBucket(HeadBucketRequest.builder()
          .bucket(bucketName)
          .build());
      return Health.up()
          .withDetail("bucket", bucketName)
          .withDetail("status", "accessible")
          .build();
    } catch (Exception e) {
      log.warn("S3 health check failed: {}", e.getMessage());
      return Health.down()
          .withDetail("bucket", bucketName)
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
