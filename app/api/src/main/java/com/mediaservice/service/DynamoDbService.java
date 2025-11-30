package com.mediaservice.service;

import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;
import software.amazon.awssdk.services.dynamodb.model.ScanRequest;
import software.amazon.awssdk.services.dynamodb.model.UpdateItemRequest;

import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

@Slf4j
@Service
@RequiredArgsConstructor
public class DynamoDbService {
  private static final String PK_PREFIX = "MEDIA#";
  private static final String SK_METADATA = "METADATA";

  private final DynamoDbClient dynamoDbClient;

  @Value("${aws.dynamodb.table-name}")
  private String tableName;

  private Map<String, AttributeValue> buildKey(String mediaId) {
    return Map.of("PK", s(PK_PREFIX + mediaId), "SK", s(SK_METADATA));
  }

  private Media mapToMedia(String mediaId, Map<String, AttributeValue> item) {
    var builder = Media.builder()
        .mediaId(mediaId)
        .size(Long.parseLong(item.get("size").n()))
        .name(item.get("name").s())
        .mimetype(item.get("mimetype").s())
        .status(MediaStatus.valueOf(item.get("status").s()))
        .width(Integer.parseInt(item.get("width").n()))
        .createdAt(Instant.parse(item.get("createdAt").s()))
        .updatedAt(Instant.parse(item.get("updatedAt").s()));
    if (item.containsKey("outputFormat")) {
      builder.outputFormat(OutputFormat.fromString(item.get("outputFormat").s()));
    }
    return builder.build();
  }

  public void createMedia(Media media) {
    var now = Instant.now().toString();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(PK_PREFIX + media.getMediaId()));
    item.put("SK", s(SK_METADATA));
    item.put("size", n(String.valueOf(media.getSize())));
    item.put("name", s(media.getName()));
    item.put("mimetype", s(media.getMimetype()));
    item.put("status", s(media.getStatus() != null ? media.getStatus().name() : MediaStatus.PENDING.name()));
    item.put("width", n(String.valueOf(media.getWidth())));
    item.put("outputFormat", s(media.getOutputFormat() != null ? media.getOutputFormat().getFormat() : OutputFormat.JPEG.getFormat()));
    item.put("createdAt", s(now));
    item.put("updatedAt", s(now));

    var request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();
    dynamoDbClient.putItem(request);
    log.info("Created media record for mediaId: {}", media.getMediaId());
  }

  public Optional<Media> getMedia(String mediaId) {
    var request = GetItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(mediaId))
        .build();
    var response = dynamoDbClient.getItem(request);
    if (!response.hasItem() || response.item().isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(mapToMedia(mediaId, response.item()));
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }

  public List<Media> getAllMedia() {
    var mediaList = new ArrayList<Media>();
    var request = ScanRequest.builder()
        .tableName(tableName)
        .filterExpression("SK = :sk")
        .expressionAttributeValues(Map.of(":sk", s(SK_METADATA)))
        .build();

    var response = dynamoDbClient.scan(request);
    for (var item : response.items()) {
      var pk = item.get("PK").s();
      var mediaId = pk.replace(PK_PREFIX, "");
      mediaList.add(mapToMedia(mediaId, item));
    }
    mediaList.sort((a, b) -> b.getCreatedAt().compareTo(a.getCreatedAt()));
    log.info("Retrieved {} media records", mediaList.size());
    return mediaList;
  }

  public void updateStatus(String mediaId, MediaStatus status) {
    var request = UpdateItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(mediaId))
        .updateExpression("SET #status = :status, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())))
        .build();
    dynamoDbClient.updateItem(request);
    log.info("Updated status for mediaId: {} to {}", mediaId, status);
  }

  public boolean updateStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus) {
    try {
      var request = UpdateItemRequest.builder()
          .tableName(tableName)
          .key(buildKey(mediaId))
          .updateExpression("SET #status = :newStatus, updatedAt = :updatedAt")
          .conditionExpression("#status = :expectedStatus")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":newStatus", s(newStatus.name()),
              ":expectedStatus", s(expectedStatus.name()),
              ":updatedAt", s(Instant.now().toString())))
          .build();
      dynamoDbClient.updateItem(request);
      log.info("Updated status for mediaId: {} from {} to {}", mediaId, expectedStatus, newStatus);
      return true;
    } catch (ConditionalCheckFailedException e) {
      log.warn("Conditional update failed for mediaId: {}, expected status: {}", mediaId, expectedStatus);
      return false;
    }
  }
}
