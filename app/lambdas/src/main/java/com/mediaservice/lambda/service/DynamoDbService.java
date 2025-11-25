package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.model.Media;
import com.mediaservice.lambda.model.MediaStatus;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;

import java.util.Map;
import java.util.Optional;

public class DynamoDbService {

  private static final Logger logger = LoggerFactory.getLogger(DynamoDbService.class);
  private static final String TABLE_NAME = System.getenv("MEDIA_DYNAMODB_TABLE_NAME") != null
      ? System.getenv("MEDIA_DYNAMODB_TABLE_NAME")
      : "media";

  private final DynamoDbClient client;

  public DynamoDbService() {
    this.client = AwsClientFactory.getDynamoDbClient();
  }

  public Optional<Media> setMediaStatusConditionally(String mediaId, MediaStatus newStatus,
      MediaStatus expectedStatus) {
    var key = Map.of(
        "PK", s("MEDIA#" + mediaId),
        "SK", s("METADATA")
    );

    var expressionValues = Map.of(
        ":newStatus", s(newStatus.name()),
        ":expectedStatus", s(expectedStatus.name())
    );

    var expressionNames = Map.of("#status", "status");

    UpdateItemRequest request = UpdateItemRequest.builder()
        .tableName(TABLE_NAME)
        .key(key)
        .updateExpression("SET #status = :newStatus")
        .conditionExpression("#status = :expectedStatus")
        .expressionAttributeNames(expressionNames)
        .expressionAttributeValues(expressionValues)
        .returnValues(ReturnValue.ALL_NEW)
        .build();

    try {
      UpdateItemResponse response = client.updateItem(request);
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
    } catch (ConditionalCheckFailedException e) {
      logger.warn("Conditional check failed for mediaId: {}", mediaId);
      throw e;
    } catch (DynamoDbException e) {
      logger.error("Failed to update media status: {}", e.getMessage());
      throw e;
    }
  }

  public void setMediaStatus(String mediaId, MediaStatus newStatus) {
    var key = Map.of(
        "PK", s("MEDIA#" + mediaId),
        "SK", s("METADATA")
    );

    var expressionValues = Map.of(
        ":newStatus", s(newStatus.name())
    );

    var expressionNames = Map.of("#status", "status");

    UpdateItemRequest request = UpdateItemRequest.builder()
        .tableName(TABLE_NAME)
        .key(key)
        .updateExpression("SET #status = :newStatus")
        .expressionAttributeNames(expressionNames)
        .expressionAttributeValues(expressionValues)
        .build();

    try {
      client.updateItem(request);
      logger.info("Updated media status to {} for mediaId: {}", newStatus, mediaId);
    } catch (DynamoDbException e) {
      logger.error("Failed to update media status: {}", e.getMessage());
      throw e;
    }
  }

  public Optional<Media> deleteMedia(String mediaId) {
    var key = Map.of(
        "PK", s("MEDIA#" + mediaId),
        "SK", s("METADATA")
    );

    DeleteItemRequest request = DeleteItemRequest.builder()
        .tableName(TABLE_NAME)
        .key(key)
        .returnValues(ReturnValue.ALL_OLD)
        .build();

    try {
      DeleteItemResponse response = client.deleteItem(request);
      Map<String, AttributeValue> attributes = response.attributes();

      if (attributes == null || attributes.isEmpty()) {
        return Optional.empty();
      }

      return Optional.of(Media.builder()
          .mediaId(mediaId)
          .name(attributes.get("name").s())
          .status(MediaStatus.valueOf(attributes.get("status").s()))
          .build());
    } catch (DynamoDbException e) {
      logger.error("Failed to delete media: {}", e.getMessage());
      throw e;
    }
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }
}
