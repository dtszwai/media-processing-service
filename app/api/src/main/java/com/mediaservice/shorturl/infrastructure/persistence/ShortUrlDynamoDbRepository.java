package com.mediaservice.shorturl.infrastructure.persistence;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.shorturl.domain.model.ShortUrl;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import java.time.Instant;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;
import software.amazon.awssdk.services.dynamodb.model.QueryRequest;

@Repository
public class ShortUrlDynamoDbRepository extends AbstractDynamoDbRepository<ShortUrl> {
  private static final int DEFAULT_LIST_LIMIT = 50;

  public ShortUrlDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  @Override
  public ShortUrl mapFromItem(Map<String, AttributeValue> item) {
    String pk = getString(item, "PK");
    String sk = getString(item, "SK");
    String code = extractCode(pk, sk);
    var expiresAtEpoch = getLong(item, "expiresAt");
    Instant expiresAt = expiresAtEpoch != null ? Instant.ofEpochSecond(expiresAtEpoch) : null;
    Boolean isPublic = getBoolean(item, "public");

    return ShortUrl.builder()
        .code(code)
        .tenantId(getString(item, "tenantId"))
        .mediaId(getString(item, "mediaId"))
        .assetId(getString(item, "assetId"))
        .isPublic(isPublic != null && isPublic)
        .createdAt(getInstant(item, "createdAt"))
        .createdBy(getString(item, "createdBy"))
        .expiresAt(expiresAt)
        .revokedAt(getInstant(item, "revokedAt"))
        .label(getString(item, "label"))
        .build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(ShortUrl shortUrl) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(StorageConstants.DYNAMO_PK_SHORT_URL_PREFIX + shortUrl.getCode()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_METADATA));
    item.put("tenantId", s(shortUrl.getTenantId()));
    item.put("mediaId", s(shortUrl.getMediaId()));
    if (shortUrl.getCreatedAt() != null) {
      item.put("createdAt", s(shortUrl.getCreatedAt().toString()));
    } else {
      item.put("createdAt", s(now.toString()));
    }
    putSharedAttributes(item, shortUrl);
    return item;
  }

  public Optional<ShortUrl> getByCode(String code) {
    return findByKey(StorageConstants.DYNAMO_PK_SHORT_URL_PREFIX + code, StorageConstants.DYNAMO_SK_METADATA);
  }

  public void createShortUrl(ShortUrl shortUrl) {
    var item = mapToItem(shortUrl);
    var request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .conditionExpression("attribute_not_exists(PK)")
        .build();
    dynamoDbClient.putItem(request);
    try {
      putMediaIndex(shortUrl);
    } catch (Exception e) {
      delete(StorageConstants.DYNAMO_PK_SHORT_URL_PREFIX + shortUrl.getCode(), StorageConstants.DYNAMO_SK_METADATA);
      throw e;
    }
  }

  public List<ShortUrl> listByMedia(String mediaId, Integer limit) {
    int pageSize = (limit != null && limit > 0 && limit <= 100) ? limit : DEFAULT_LIST_LIMIT;
    var request = QueryRequest.builder()
        .tableName(tableName)
        .keyConditionExpression("PK = :pk")
        .expressionAttributeValues(Map.of(
            ":pk", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId)))
        .limit(pageSize)
        .build();
    var response = dynamoDbClient.query(request);
    return response.items().stream()
        .filter(item -> {
          String sk = getString(item, "SK");
          return sk != null && sk.startsWith(StorageConstants.DYNAMO_SK_SHORT_URL_PREFIX);
        })
        .map(this::mapFromItem)
        .toList();
  }

  public void revokeShortUrl(String code, Instant revokedAt, String mediaId) {
    updateAttributes(
        StorageConstants.DYNAMO_PK_SHORT_URL_PREFIX + code,
        StorageConstants.DYNAMO_SK_METADATA,
        "SET revokedAt = :revokedAt",
        null,
        Map.of(":revokedAt", s(revokedAt.toString())));

    if (mediaId != null && !mediaId.isBlank()) {
      updateAttributes(
          StorageConstants.DYNAMO_PK_PREFIX + mediaId,
          StorageConstants.DYNAMO_SK_SHORT_URL_PREFIX + code,
          "SET revokedAt = :revokedAt",
          null,
          Map.of(":revokedAt", s(revokedAt.toString())));
    }
  }

  private void putMediaIndex(ShortUrl shortUrl) {
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(StorageConstants.DYNAMO_PK_PREFIX + shortUrl.getMediaId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_SHORT_URL_PREFIX + shortUrl.getCode()));
    item.put("tenantId", s(shortUrl.getTenantId()));
    item.put("mediaId", s(shortUrl.getMediaId()));
    if (shortUrl.getCreatedAt() != null) {
      item.put("createdAt", s(shortUrl.getCreatedAt().toString()));
    }
    putSharedAttributes(item, shortUrl);
    var request = PutItemRequest.builder()
        .tableName(tableName)
        .item(item)
        .build();
    dynamoDbClient.putItem(request);
  }

  private String extractCode(String pk, String sk) {
    if (pk != null && pk.startsWith(StorageConstants.DYNAMO_PK_SHORT_URL_PREFIX)) {
      return pk.replace(StorageConstants.DYNAMO_PK_SHORT_URL_PREFIX, "");
    }
    if (sk != null && sk.startsWith(StorageConstants.DYNAMO_SK_SHORT_URL_PREFIX)) {
      return sk.replace(StorageConstants.DYNAMO_SK_SHORT_URL_PREFIX, "");
    }
    return null;
  }

  private void putSharedAttributes(Map<String, AttributeValue> item, ShortUrl shortUrl) {
    if (shortUrl.getAssetId() != null) {
      item.put("assetId", s(shortUrl.getAssetId()));
    }
    item.put("public", bool(shortUrl.isPublic()));
    if (shortUrl.getCreatedBy() != null) {
      item.put("createdBy", s(shortUrl.getCreatedBy()));
    }
    if (shortUrl.getExpiresAt() != null) {
      item.put("expiresAt", n(String.valueOf(shortUrl.getExpiresAt().getEpochSecond())));
    }
    if (shortUrl.getRevokedAt() != null) {
      item.put("revokedAt", s(shortUrl.getRevokedAt().toString()));
    }
    if (shortUrl.getLabel() != null) {
      item.put("label", s(shortUrl.getLabel()));
    }
  }
}
