package com.mediaservice.shared.health;

import lombok.extern.slf4j.Slf4j;
import org.springframework.boot.actuate.health.Health;
import org.springframework.boot.actuate.health.HealthIndicator;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sns.model.GetTopicAttributesRequest;

import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Health indicator for SNS topic connectivity.
 *
 * <p>
 * Checks if the configured SNS topic exists and is accessible by
 * performing a GetTopicAttributes request. Results are cached for 30 seconds.
 */
@Slf4j
public class SnsHealthIndicator implements HealthIndicator {
  private final SnsClient snsClient;
  private final String topicArn;
  private final String name;
  private final AtomicReference<CachedHealth> cachedHealth = new AtomicReference<>();
  private static final Duration CACHE_TTL = Duration.ofSeconds(30);

  public SnsHealthIndicator(SnsClient snsClient, String topicArn, String name) {
    this.snsClient = snsClient;
    this.topicArn = topicArn;
    this.name = name;
  }

  @Override
  public Health health() {
    if (topicArn == null || topicArn.isBlank()) {
      return Health.unknown()
          .withDetail("reason", name + " SNS topic ARN not configured")
          .build();
    }

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
      var attributes = snsClient.getTopicAttributes(GetTopicAttributesRequest.builder()
          .topicArn(topicArn)
          .build())
          .attributes();
      return Health.up()
          .withDetail("topicArn", topicArn)
          .withDetail("subscriptionCount", attributes.get("SubscriptionsConfirmed"))
          .build();
    } catch (Exception e) {
      log.warn("{} SNS health check failed: {}", name, e.getMessage());
      return Health.down()
          .withDetail("topicArn", topicArn)
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
