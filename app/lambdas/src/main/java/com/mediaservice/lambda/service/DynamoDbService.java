package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.DeleteItemRequest;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
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

  public Optional<Media> getMedia(String mediaId) {
    var request = GetItemRequest.builder()
        .tableName(tableName)
        .key(keyFor(mediaId))
        .build();
    return toMedia(mediaId, client.getItem(request).item());
  }

  private Map<String, AttributeValue> keyFor(String mediaId) {
    return Map.of("PK", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId), "SK", s(StorageConstants.DYNAMO_SK_METADATA));
  }

  private Optional<Media> toMedia(String mediaId, Map<String, AttributeValue> attrs) {
    if (attrs == null || attrs.isEmpty()) {
      return Optional.empty();
    }
    var builder = Media.builder()
        .mediaId(mediaId)
        .name(attrs.get("name").s())
        .status(MediaStatus.valueOf(attrs.get("status").s()));
    getString(attrs, StorageConstants.DYNAMO_ATTR_TENANT_ID).ifPresent(builder::tenantId);
    getString(attrs, StorageConstants.DYNAMO_ATTR_USER_ID).ifPresent(builder::userId);
    getInt(attrs, "width").ifPresent(builder::width);
    getString(attrs, "outputFormat").map(OutputFormat::fromString).ifPresent(builder::outputFormat);
    getString(attrs, "deletedAt").map(Instant::parse).ifPresent(builder::deletedAt);
    getString(attrs, "webhookUrl").ifPresent(builder::webhookUrl);
    getString(attrs, "mimetype").ifPresent(builder::mimetype);
    getLong(attrs, "size").ifPresent(builder::size);
    return Optional.of(builder.build());
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(Integer value) {
    return AttributeValue.builder().n(String.valueOf(value)).build();
  }

  private Optional<String> getString(Map<String, AttributeValue> attrs, String key) {
    return attrs.containsKey(key) ? Optional.of(attrs.get(key).s()) : Optional.empty();
  }

  private Optional<Integer> getInt(Map<String, AttributeValue> attrs, String key) {
    return attrs.containsKey(key) ? Optional.of(Integer.parseInt(attrs.get(key).n())) : Optional.empty();
  }

  private Optional<Long> getLong(Map<String, AttributeValue> attrs, String key) {
    return attrs.containsKey(key) ? Optional.of(Long.parseLong(attrs.get(key).n())) : Optional.empty();
  }
}
