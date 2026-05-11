package com.mediaservice.generation.infrastructure;

import com.mediaservice.common.constants.StorageConstants;
import java.time.Instant;
import java.util.HashMap;
import java.util.Map;
import java.util.Optional;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.DeleteItemRequest;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;

/** DynamoDB-backed CRUD for the simulator control row consumed by SimulatedGpuProvider. */
@Component
public class SimulatorControlStore {
  private static final Map<String, AttributeValue> KEY = Map.of(
      "PK", AttributeValue.builder().s(StorageConstants.DYNAMO_PK_GENERATION_CONTROL).build(),
      "SK", AttributeValue.builder().s(StorageConstants.DYNAMO_SK_GEN_CONTROL_SIMULATOR).build());

  private final DynamoDbClient dynamoDbClient;
  private final String tableName;

  public SimulatorControlStore(DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    this.dynamoDbClient = dynamoDbClient;
    this.tableName = tableName;
  }

  public record Snapshot(Boolean paused, Double meanDurationMs, Double failureRate, String updatedAt) {
  }

  public Optional<Snapshot> get() {
    var item = dynamoDbClient.getItem(GetItemRequest.builder()
        .tableName(tableName).key(KEY).build()).item();
    if (item == null || item.isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(new Snapshot(boolValue(item, "paused"), number(item, "meanDurationMs"),
        number(item, "failureRate"), string(item, "updatedAt")));
  }

  public void update(Boolean paused, Long meanDurationMs, Double failureRate) {
    Map<String, AttributeValue> item = new HashMap<>(KEY);
    item.put("updatedAt", AttributeValue.builder().s(Instant.now().toString()).build());
    if (paused != null) {
      item.put("paused", AttributeValue.builder().bool(paused).build());
    }
    if (meanDurationMs != null) {
      item.put("meanDurationMs", AttributeValue.builder().n(String.valueOf(Math.max(0, meanDurationMs))).build());
    }
    if (failureRate != null) {
      double normalized = Math.max(0.0, Math.min(1.0, failureRate));
      item.put("failureRate", AttributeValue.builder().n(String.valueOf(normalized)).build());
    }
    dynamoDbClient.putItem(PutItemRequest.builder().tableName(tableName).item(item).build());
  }

  public void clear() {
    dynamoDbClient.deleteItem(DeleteItemRequest.builder().tableName(tableName).key(KEY).build());
  }

  private static String string(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) && item.get(key).s() != null ? item.get(key).s() : null;
  }

  private static Boolean boolValue(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) ? item.get(key).bool() : null;
  }

  private static Double number(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) && item.get(key).n() != null ? Double.parseDouble(item.get(key).n()) : null;
  }
}
