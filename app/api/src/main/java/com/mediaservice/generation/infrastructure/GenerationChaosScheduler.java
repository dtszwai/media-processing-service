package com.mediaservice.generation.infrastructure;

import java.time.Instant;
import java.time.LocalTime;
import java.time.ZoneOffset;
import com.mediaservice.common.constants.StorageConstants;
import java.util.Map;
import lombok.RequiredArgsConstructor;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.UpdateItemRequest;

@Component
@RequiredArgsConstructor
@ConditionalOnProperty(name = "media.generation.chaos.enabled", havingValue = "true")
public class GenerationChaosScheduler {
  private static final Map<String, AttributeValue> CONTROL_KEY = Map.of(
      "PK", AttributeValue.builder().s(StorageConstants.DYNAMO_PK_GENERATION_CONTROL).build(),
      "SK", AttributeValue.builder().s(StorageConstants.DYNAMO_SK_GEN_CONTROL_SIMULATOR).build());

  private final DynamoDbClient dynamoDbClient;

  @Value("${aws.dynamodb.table-name}")
  private String tableName;

  @Value("${media.generation.chaos.failure-rate:0.05}")
  private double failureRate;

  @Value("${media.generation.chaos.start-hour-utc:16}")
  private int startHourUtc;

  @Value("${media.generation.chaos.end-hour-utc:1}")
  private int endHourUtc;

  private volatile Double lastWrittenRate = null;

  @Scheduled(fixedDelayString = "${media.generation.chaos.refresh-ms:60000}")
  public void refreshSimulatorChaosControl() {
    double activeFailureRate = inBusinessWindow() ? Math.max(0.0, Math.min(1.0, failureRate)) : 0.0;
    if (lastWrittenRate != null && lastWrittenRate == activeFailureRate) {
      return;
    }
    // Update only the chaos-managed fields; leave `paused` untouched so admin overrides persist.
    // Use ExpressionAttributeNames for reserved-word safety.
    dynamoDbClient.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(CONTROL_KEY)
        .updateExpression("SET #failureRate = :rate, #updatedAt = :ts, #chaosManaged = :true")
        .expressionAttributeNames(Map.of(
            "#failureRate", "failureRate",
            "#updatedAt", "updatedAt",
            "#chaosManaged", "chaosManaged"))
        .expressionAttributeValues(Map.of(
            ":rate", AttributeValue.builder().n(String.valueOf(activeFailureRate)).build(),
            ":ts", AttributeValue.builder().s(Instant.now().toString()).build(),
            ":true", AttributeValue.builder().bool(true).build()))
        .build());
    lastWrittenRate = activeFailureRate;
  }

  private boolean inBusinessWindow() {
    int hour = LocalTime.now(ZoneOffset.UTC).getHour();
    int start = Math.floorMod(startHourUtc, 24);
    int end = Math.floorMod(endHourUtc, 24);
    return start <= end ? hour >= start && hour < end : hour >= start || hour < end;
  }
}
