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
  private static final String PK_PREFIX = "MEDIA#";
  private static final String SK_METADATA = "METADATA";

  private final DynamoDbClient dynamoDbClient;

  @Value("${aws.dynamodb.table-name}")
  private String tableName;

  private Map<String, AttributeValue> buildKey(String mediaId) {
    return Map.of("PK", s(PK_PREFIX + mediaId), "SK", s(SK_METADATA));
  }

  private Media mapToMedia(String mediaId, Map<String, AttributeValue> item) {
    return Media.builder()
        .mediaId(mediaId)
        .size(item.containsKey("size") ? Long.parseLong(item.get("size").n()) : null)
        .name(item.get("name").s())
        .mimetype(item.containsKey("mimetype") ? item.get("mimetype").s() : null)
        .status(MediaStatus.valueOf(item.get("status").s()))
        .width(item.containsKey("width") ? Integer.parseInt(item.get("width").n()) : null)
        .build();
  }

  public void createMedia(Media media) {
    var item = Map.of(
        "PK", s(PK_PREFIX + media.getMediaId()),
        "SK", s(SK_METADATA),
        "size", n(String.valueOf(media.getSize())),
        "name", s(media.getName()),
        "mimetype", s(media.getMimetype()),
        "status", s(MediaStatus.PENDING.name()),
        "width", n(String.valueOf(media.getWidth())));
    PutItemRequest request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();
    dynamoDbClient.putItem(request);
    log.info("Created media record for mediaId: {}", media.getMediaId());
  }

  public Optional<Media> getMedia(String mediaId) {
    GetItemRequest request = GetItemRequest.builder()
        .tableName(tableName)
        .key(buildKey(mediaId))
        .build();
    GetItemResponse response = dynamoDbClient.getItem(request);
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
}
