package com.mediaservice.health;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.actuate.health.Health;
import org.springframework.boot.actuate.health.HealthIndicator;
import org.springframework.stereotype.Component;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.DescribeTableRequest;

import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Health indicator for DynamoDB table connectivity.
 *
 * <p>Checks if the configured DynamoDB table exists and is accessible by
 * performing a DescribeTable request. Results are cached for 30 seconds.
 */
@Component
@Slf4j
public class DynamoDbHealthIndicator implements HealthIndicator {
  private final DynamoDbClient dynamoDbClient;
  private final AtomicReference<CachedHealth> cachedHealth = new AtomicReference<>();
  private static final Duration CACHE_TTL = Duration.ofSeconds(30);

  @Value("${aws.dynamodb.table-name}")
  private String tableName;

  public DynamoDbHealthIndicator(DynamoDbClient dynamoDbClient) {
    this.dynamoDbClient = dynamoDbClient;
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
      var table = dynamoDbClient.describeTable(DescribeTableRequest.builder()
              .tableName(tableName)
              .build())
          .table();
      return Health.up()
          .withDetail("table", tableName)
          .withDetail("status", table.tableStatusAsString())
          .withDetail("itemCount", table.itemCount())
          .build();
    } catch (Exception e) {
      log.warn("DynamoDB health check failed: {}", e.getMessage());
      return Health.down()
          .withDetail("table", tableName)
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
