package com.mediaservice.service;

import com.mediaservice.model.Media;
import com.mediaservice.model.MediaStatus;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;

import java.util.Map;
import java.util.Optional;

@Slf4j
@Service
@RequiredArgsConstructor
public class DynamoDbService {

  private final DynamoDbClient dynamoDbClient;

  @Value("${aws.dynamodb.table-name}")
  private String tableName;

  public void createMedia(Media media) {
    var item = Map.of(
        "PK", s("MEDIA#" + media.getMediaId()),
        "SK", s("METADATA"),
        "size", n(String.valueOf(media.getSize())),
        "name", s(media.getName()),
        "mimetype", s(media.getMimetype()),
        "status", s(MediaStatus.PENDING.name()),
        "width", n(String.valueOf(media.getWidth()))
    );

    PutItemRequest request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();

    try {
      dynamoDbClient.putItem(request);
      log.info("Created media record for mediaId: {}", media.getMediaId());
    } catch (DynamoDbException e) {
      log.error("Failed to create media record: {}", e.getMessage());
      throw e;
    }
  }

  public Optional<Media> getMedia(String mediaId) {
    var key = Map.of(
        "PK", s("MEDIA#" + mediaId),
        "SK", s("METADATA")
    );

    GetItemRequest request = GetItemRequest.builder()
        .tableName(tableName)
        .key(key)
        .build();

    try {
      GetItemResponse response = dynamoDbClient.getItem(request);

      if (!response.hasItem() || response.item().isEmpty()) {
        return Optional.empty();
      }

      Map<String, AttributeValue> item = response.item();
      return Optional.of(Media.builder()
          .mediaId(mediaId)
          .size(Long.parseLong(item.get("size").n()))
          .name(item.get("name").s())
          .mimetype(item.get("mimetype").s())
          .status(MediaStatus.valueOf(item.get("status").s()))
          .build());
    } catch (DynamoDbException e) {
      log.error("Failed to get media record: {}", e.getMessage());
      throw e;
    }
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
        .tableName(tableName)
        .key(key)
        .updateExpression("SET #status = :newStatus")
        .conditionExpression("#status = :expectedStatus")
        .expressionAttributeNames(expressionNames)
        .expressionAttributeValues(expressionValues)
        .returnValues(ReturnValue.ALL_NEW)
        .build();

    try {
      UpdateItemResponse response = dynamoDbClient.updateItem(request);
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
      log.warn("Conditional check failed for mediaId: {}", mediaId);
      throw e;
    } catch (DynamoDbException e) {
      log.error("Failed to update media status: {}", e.getMessage());
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
        .tableName(tableName)
        .key(key)
        .updateExpression("SET #status = :newStatus")
        .expressionAttributeNames(expressionNames)
        .expressionAttributeValues(expressionValues)
        .build();

    try {
      dynamoDbClient.updateItem(request);
      log.info("Updated media status to {} for mediaId: {}", newStatus, mediaId);
    } catch (DynamoDbException e) {
      log.error("Failed to update media status: {}", e.getMessage());
      throw e;
    }
  }

  public Optional<Media> deleteMedia(String mediaId) {
    var key = Map.of(
        "PK", s("MEDIA#" + mediaId),
        "SK", s("METADATA")
    );

    DeleteItemRequest request = DeleteItemRequest.builder()
        .tableName(tableName)
        .key(key)
        .returnValues(ReturnValue.ALL_OLD)
        .build();

    try {
      DeleteItemResponse response = dynamoDbClient.deleteItem(request);
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
      log.error("Failed to delete media: {}", e.getMessage());
      throw e;
    }
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }
}
