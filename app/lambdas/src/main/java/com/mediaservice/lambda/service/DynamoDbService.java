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
    var key = Map.of("PK", s("MEDIA#" + mediaId), "SK", s("METADATA"));
    var expressionValues = Map.of(":newStatus", s(newStatus.name()), ":expectedStatus", s(expectedStatus.name()));
    var expressionNames = Map.of("#status", "status");
    var request = UpdateItemRequest.builder()
        .tableName(tableName)
        .key(key)
        .updateExpression("SET #status = :newStatus")
        .conditionExpression("#status = :expectedStatus")
        .expressionAttributeNames(expressionNames)
        .expressionAttributeValues(expressionValues)
        .returnValues(ReturnValue.ALL_NEW)
        .build();
    var response = client.updateItem(request);
    Map<String, AttributeValue> attributes = response.attributes();
    if (attributes == null || attributes.isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(Media.builder()
        .mediaId(mediaId)
        .name(attributes.get("name").s())
        .status(MediaStatus.valueOf(attributes.get("status").s()))
        .width(Integer.parseInt(attributes.get("width").n()))
        .build());
  }

  public void setMediaStatus(String mediaId, MediaStatus newStatus) {
    var key = Map.of("PK", s("MEDIA#" + mediaId), "SK", s("METADATA"));
    var expressionValues = Map.of(":newStatus", s(newStatus.name()));
    var expressionNames = Map.of("#status", "status");
    var request = UpdateItemRequest.builder()
        .tableName(tableName)
        .key(key)
        .updateExpression("SET #status = :newStatus")
        .expressionAttributeNames(expressionNames)
        .expressionAttributeValues(expressionValues)
        .build();
    client.updateItem(request);
  }

  public Optional<Media> deleteMedia(String mediaId) {
    var key = Map.of("PK", s("MEDIA#" + mediaId), "SK", s("METADATA"));
    var request = DeleteItemRequest.builder()
        .tableName(tableName)
        .key(key)
        .returnValues(ReturnValue.ALL_OLD)
        .build();
    var response = client.deleteItem(request);
    Map<String, AttributeValue> attributes = response.attributes();
    if (attributes == null || attributes.isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(Media.builder()
        .mediaId(mediaId)
        .name(attributes.get("name").s())
        .status(MediaStatus.valueOf(attributes.get("status").s()))
        .build());
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }
}
