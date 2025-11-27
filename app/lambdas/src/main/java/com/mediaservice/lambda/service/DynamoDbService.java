package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.lambda.model.Media;
import com.mediaservice.lambda.model.MediaStatus;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.DeleteItemRequest;
import software.amazon.awssdk.services.dynamodb.model.ReturnValue;
import software.amazon.awssdk.services.dynamodb.model.UpdateItemRequest;

import java.time.Instant;
import java.util.HashMap;
import java.util.Map;
import java.util.Optional;

public class DynamoDbService {
  private final DynamoDbClient client;
  private final String tableName;

  public DynamoDbService() {
    this.client = AwsClientFactory.getDynamoDbClient();
    this.tableName = LambdaConfig.getInstance().getTableName();
  }

  public Optional<Media> setMediaStatusConditionally(String mediaId, MediaStatus newStatus,
      MediaStatus expectedStatus) {
    return setMediaStatusConditionally(mediaId, newStatus, expectedStatus, null);
  }

  public Optional<Media> setMediaStatusConditionally(String mediaId, MediaStatus newStatus,
      MediaStatus expectedStatus, Integer width) {
    var values = new HashMap<>(Map.of(
        ":newStatus", s(newStatus.name()),
        ":expectedStatus", s(expectedStatus.name()),
        ":updatedAt", s(Instant.now().toString())));
    if (width != null) {
      values.put(":width", n(width));
    }
    return toMedia(mediaId, client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(keyFor(mediaId))
        .updateExpression(
            "SET #status = :newStatus, updatedAt = :updatedAt" + (width != null ? ", width = :width" : ""))
        .conditionExpression("#status = :expectedStatus")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(values)
        .returnValues(ReturnValue.ALL_NEW)
        .build()).attributes());
  }

  public void setMediaStatus(String mediaId, MediaStatus newStatus) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(keyFor(mediaId))
        .updateExpression("SET #status = :newStatus, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(":newStatus", s(newStatus.name()), ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  public Optional<Media> deleteMedia(String mediaId) {
    var request = DeleteItemRequest.builder()
        .tableName(tableName)
        .key(keyFor(mediaId))
        .returnValues(ReturnValue.ALL_OLD)
        .build();
    return toMedia(mediaId, client.deleteItem(request).attributes());
  }

  private Map<String, AttributeValue> keyFor(String mediaId) {
    return Map.of("PK", s("MEDIA#" + mediaId), "SK", s("METADATA"));
  }

  private Optional<Media> toMedia(String mediaId, Map<String, AttributeValue> attrs) {
    if (attrs == null || attrs.isEmpty()) {
      return Optional.empty();
    }
    var builder = Media.builder()
        .mediaId(mediaId)
        .name(attrs.get("name").s())
        .status(MediaStatus.valueOf(attrs.get("status").s()));
    if (attrs.containsKey("width")) {
      builder.width(Integer.parseInt(attrs.get("width").n()));
    }
    return Optional.of(builder.build());
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(Integer value) {
    return AttributeValue.builder().n(String.valueOf(value)).build();
  }
}
