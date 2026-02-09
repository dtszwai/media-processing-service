package com.mediaservice.media.infrastructure.persistence;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.QueryRequest;

@Slf4j
@Repository
public class MediaAssetDynamoDbRepository extends AbstractDynamoDbRepository<MediaAsset> {
  public MediaAssetDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  @Override
  public MediaAsset mapFromItem(Map<String, AttributeValue> item) {
    var pk = getString(item, "PK");
    var mediaId = pk.replace(StorageConstants.DYNAMO_PK_PREFIX, "");
    return MediaAsset.builder()
        .assetId(getString(item, "assetId"))
        .mediaId(mediaId)
        .tenantId(getString(item, StorageConstants.DYNAMO_ATTR_TENANT_ID))
        .sourceAssetId(getString(item, "sourceAssetId"))
        .type(parseAssetType(getString(item, "type")))
        .tags(getStringList(item, "tags"))
        .status(parseAssetStatus(getString(item, "status")))
        .outputFormat(getString(item, "outputFormat"))
        .mimetype(getString(item, "mimetype"))
        .size(getLong(item, "size"))
        .width(getInt(item, "width"))
        .height(getInt(item, "height"))
        .downloadName(getString(item, "downloadName"))
        .operation(parseOperation(getString(item, "operation")))
        .createdAt(getInstant(item, "createdAt"))
        .updatedAt(getInstant(item, "updatedAt"))
        .errorMessage(getString(item, "errorMessage"))
        .build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(MediaAsset asset) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(StorageConstants.DYNAMO_PK_PREFIX + asset.getMediaId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX + asset.getAssetId()));
    item.put("assetId", s(asset.getAssetId()));
    if (asset.getTenantId() != null) {
      item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(asset.getTenantId()));
    }
    if (asset.getSourceAssetId() != null) {
      item.put("sourceAssetId", s(asset.getSourceAssetId()));
    }
    if (asset.getType() != null) {
      item.put("type", s(asset.getType().name()));
    }
    if (asset.getTags() != null && !asset.getTags().isEmpty()) {
      item.put("tags", AttributeValue.builder().ss(asset.getTags()).build());
    }
    item.put("status", s(asset.getStatus() != null ? asset.getStatus().name() : AssetStatus.PENDING.name()));
    if (asset.getOutputFormat() != null) {
      item.put("outputFormat", s(asset.getOutputFormat()));
    }
    if (asset.getMimetype() != null) {
      item.put("mimetype", s(asset.getMimetype()));
    }
    if (asset.getSize() != null) {
      item.put("size", n(String.valueOf(asset.getSize())));
    }
    if (asset.getWidth() != null) {
      item.put("width", n(String.valueOf(asset.getWidth())));
    }
    if (asset.getHeight() != null) {
      item.put("height", n(String.valueOf(asset.getHeight())));
    }
    if (asset.getDownloadName() != null) {
      item.put("downloadName", s(asset.getDownloadName()));
    }
    if (asset.getOperation() != null) {
      item.put("operation", s(asset.getOperation().getValue()));
    }
    item.put("createdAt", s(asset.getCreatedAt() != null ? asset.getCreatedAt().toString() : now.toString()));
    item.put("updatedAt", s(now.toString()));
    if (asset.getErrorMessage() != null) {
      item.put("errorMessage", s(asset.getErrorMessage()));
    }
    return item;
  }

  public void createAsset(MediaAsset asset) {
    save(asset);
    log.info("Created asset record assetId={} for mediaId={}", asset.getAssetId(), asset.getMediaId());
  }

  public Optional<MediaAsset> getAsset(String mediaId, String assetId) {
    return findByKey(StorageConstants.DYNAMO_PK_PREFIX + mediaId, StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId);
  }

  public List<MediaAsset> listAssets(String mediaId) {
    var request = QueryRequest.builder()
        .tableName(tableName)
        .keyConditionExpression("PK = :pk AND begins_with(SK, :skPrefix)")
        .expressionAttributeValues(Map.of(
            ":pk", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId),
            ":skPrefix", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX)))
        .build();
    var response = dynamoDbClient.query(request);
    var results = new ArrayList<MediaAsset>();
    for (var item : response.items()) {
      results.add(mapFromItem(item));
    }
    return results;
  }

  public boolean updateStatusConditionally(String mediaId, String assetId, AssetStatus newStatus, AssetStatus expectedStatus) {
    return updateConditionally(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId,
        "SET #status = :newStatus, updatedAt = :updatedAt REMOVE errorMessage",
        "#status = :expectedStatus",
        Map.of("#status", "status"),
        Map.of(
            ":newStatus", s(newStatus.name()),
            ":expectedStatus", s(expectedStatus.name()),
            ":updatedAt", s(Instant.now().toString())));
  }

  public void updateStatus(String mediaId, String assetId, AssetStatus status) {
    updateAttributes(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId,
        "SET #status = :status, updatedAt = :updatedAt REMOVE errorMessage",
        Map.of("#status", "status"),
        Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())));
  }

  private AssetStatus parseAssetStatus(String value) {
    return value != null ? AssetStatus.valueOf(value) : null;
  }

  private AssetType parseAssetType(String value) {
    return value != null ? AssetType.valueOf(value) : null;
  }

  private AssetOperation parseOperation(String value) {
    return AssetOperation.fromString(value);
  }

  private List<String> getStringList(Map<String, AttributeValue> item, String key) {
    if (!item.containsKey(key)) {
      return null;
    }
    var attr = item.get(key);
    return attr.ss() != null ? new ArrayList<>(attr.ss()) : null;
  }
}
