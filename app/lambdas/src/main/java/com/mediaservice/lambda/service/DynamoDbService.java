package com.mediaservice.lambda.service;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaSource;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.common.model.ProcessingJobStatus;
import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.lambda.service.DocumentProcessingService.DocumentMetadata;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.QueryRequest;
import software.amazon.awssdk.services.dynamodb.model.ReturnValue;
import software.amazon.awssdk.services.dynamodb.model.UpdateItemRequest;

public class DynamoDbService {
  private final DynamoDbClient client;
  private final String tableName;

  public DynamoDbService() {
    this.client = AwsClientFactory.getDynamoDbClient();
    this.tableName = LambdaConfig.getInstance().getTableName();
  }

  public Optional<Media> getMedia(String mediaId) {
    var request = GetItemRequest.builder()
        .tableName(tableName)
        .key(mediaKey(mediaId))
        .build();
    return toMedia(mediaId, client.getItem(request).item());
  }

  public void updateMediaStatus(String mediaId, MediaStatus status) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(mediaKey(mediaId))
        .updateExpression("SET #status = :status, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  public Optional<MediaAsset> getAsset(String mediaId, String assetId) {
    var request = GetItemRequest.builder()
        .tableName(tableName)
        .key(assetKey(mediaId, assetId))
        .build();
    return toAsset(mediaId, client.getItem(request).item());
  }

  public List<MediaAsset> listAssets(String mediaId) {
    var request = QueryRequest.builder()
        .tableName(tableName)
        .keyConditionExpression("PK = :pk AND begins_with(SK, :skPrefix)")
        .expressionAttributeValues(Map.of(
            ":pk", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId),
            ":skPrefix", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX)))
        .build();
    var response = client.query(request);
    var results = new ArrayList<MediaAsset>();
    for (var item : response.items()) {
      toAsset(mediaId, item).ifPresent(results::add);
    }
    return results;
  }

  public boolean updateAssetStatusConditionally(String mediaId, String assetId, AssetStatus newStatus, AssetStatus expectedStatus) {
    try {
      client.updateItem(UpdateItemRequest.builder()
          .tableName(tableName)
          .key(assetKey(mediaId, assetId))
          .updateExpression("SET #status = :newStatus, updatedAt = :updatedAt REMOVE errorMessage")
          .conditionExpression("#status = :expectedStatus")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":newStatus", s(newStatus.name()),
              ":expectedStatus", s(expectedStatus.name()),
              ":updatedAt", s(Instant.now().toString())))
          .build());
      return true;
    } catch (software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException e) {
      return false;
    }
  }

  public void updateAssetSuccess(String mediaId, String assetId, long size, Integer width, Integer height, String mimetype) {
    var values = new java.util.HashMap<String, AttributeValue>();
    values.put(":status", s(AssetStatus.COMPLETE.name()));
    values.put(":updatedAt", s(Instant.now().toString()));
    values.put(":size", n(size));
    values.put(":mimetype", s(mimetype != null ? mimetype : "application/octet-stream"));
    var update = new StringBuilder("SET #status = :status, updatedAt = :updatedAt, size = :size, mimetype = :mimetype");
    if (width != null) {
      update.append(", width = :width");
      values.put(":width", n(width));
    }
    if (height != null) {
      update.append(", height = :height");
      values.put(":height", n(height));
    }
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(assetKey(mediaId, assetId))
        .updateExpression(update.toString())
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(values)
        .build());
  }

  public void updateAssetError(String mediaId, String assetId, String errorMessage) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(assetKey(mediaId, assetId))
        .updateExpression("SET #status = :status, errorMessage = :errorMessage, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(AssetStatus.ERROR.name()),
            ":errorMessage", s(errorMessage),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  public boolean updateJobStatusConditionally(String mediaId, String jobId, ProcessingJobStatus newStatus, ProcessingJobStatus expectedStatus) {
    try {
      client.updateItem(UpdateItemRequest.builder()
          .tableName(tableName)
          .key(jobKey(mediaId, jobId))
          .updateExpression("SET #status = :newStatus, updatedAt = :updatedAt")
          .conditionExpression("#status = :expectedStatus")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":newStatus", s(newStatus.name()),
              ":expectedStatus", s(expectedStatus.name()),
              ":updatedAt", s(Instant.now().toString())))
          .build());
      return true;
    } catch (software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException e) {
      return false;
    }
  }

  public void updateJobError(String mediaId, String jobId, String errorMessage) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(mediaId, jobId))
        .updateExpression("SET #status = :status, errorMessage = :errorMessage, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(ProcessingJobStatus.ERROR.name()),
            ":errorMessage", s(errorMessage),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  public void updateDocumentMetadata(String mediaId, DocumentMetadata metadata) {
    var values = new java.util.HashMap<String, AttributeValue>();
    var update = new StringBuilder("SET updatedAt = :updatedAt");
    values.put(":updatedAt", s(Instant.now().toString()));

    if (metadata.pageCount() > 0) {
      update.append(", documentPageCount = :documentPageCount");
      values.put(":documentPageCount", n(metadata.pageCount()));
    }
    if (metadata.title() != null) {
      update.append(", documentTitle = :documentTitle");
      values.put(":documentTitle", s(metadata.title()));
    }
    if (metadata.author() != null) {
      update.append(", documentAuthor = :documentAuthor");
      values.put(":documentAuthor", s(metadata.author()));
    }
    if (metadata.subject() != null) {
      update.append(", documentSubject = :documentSubject");
      values.put(":documentSubject", s(metadata.subject()));
    }
    if (metadata.creator() != null) {
      update.append(", documentCreator = :documentCreator");
      values.put(":documentCreator", s(metadata.creator()));
    }
    if (metadata.producer() != null) {
      update.append(", documentProducer = :documentProducer");
      values.put(":documentProducer", s(metadata.producer()));
    }
    if (metadata.createdAt() != null) {
      update.append(", documentCreationDate = :documentCreationDate");
      values.put(":documentCreationDate", s(metadata.createdAt().toString()));
    }
    if (metadata.modifiedAt() != null) {
      update.append(", documentModifiedDate = :documentModifiedDate");
      values.put(":documentModifiedDate", s(metadata.modifiedAt().toString()));
    }
    if (metadata.textLength() != null) {
      update.append(", documentTextLength = :documentTextLength");
      values.put(":documentTextLength", n(metadata.textLength()));
    }
    if (metadata.textTruncated() != null) {
      update.append(", documentTextTruncated = :documentTextTruncated");
      values.put(":documentTextTruncated", bool(metadata.textTruncated()));
    }

    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(mediaKey(mediaId))
        .updateExpression(update.toString())
        .expressionAttributeValues(values)
        .build());
  }

  private Map<String, AttributeValue> mediaKey(String mediaId) {
    return Map.of("PK", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId), "SK", s(StorageConstants.DYNAMO_SK_MEDIA));
  }

  private Map<String, AttributeValue> assetKey(String mediaId, String assetId) {
    return Map.of(
        "PK", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId),
        "SK", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId));
  }

  private Map<String, AttributeValue> jobKey(String mediaId, String jobId) {
    return Map.of(
        "PK", s(StorageConstants.DYNAMO_PK_PREFIX + mediaId),
        "SK", s(StorageConstants.DYNAMO_SK_JOB_PREFIX + jobId));
  }

  private Optional<Media> toMedia(String mediaId, Map<String, AttributeValue> attrs) {
    if (attrs == null || attrs.isEmpty()) {
      return Optional.empty();
    }
    var builder = Media.builder()
        .mediaId(mediaId)
        .name(getString(attrs, "name").orElse(null))
        .status(getString(attrs, "status").map(MediaStatus::valueOf).orElse(null))
        .mimetype(getString(attrs, "mimetype").orElse(null))
        .originalAssetId(getString(attrs, "originalAssetId").orElse(null));
    getString(attrs, StorageConstants.DYNAMO_ATTR_TENANT_ID).ifPresent(builder::tenantId);
    getString(attrs, StorageConstants.DYNAMO_ATTR_USER_ID).ifPresent(builder::userId);
    getString(attrs, "mediaType").map(MediaType::fromString).ifPresent(builder::mediaType);
    getString(attrs, "source").map(MediaSource::fromString).ifPresent(builder::source);
    getString(attrs, "deletedAt").map(Instant::parse).ifPresent(builder::deletedAt);
    getString(attrs, "webhookUrl").ifPresent(builder::webhookUrl);
    getLong(attrs, "size").ifPresent(builder::size);
    return Optional.of(builder.build());
  }

  private Optional<MediaAsset> toAsset(String mediaId, Map<String, AttributeValue> attrs) {
    if (attrs == null || attrs.isEmpty()) {
      return Optional.empty();
    }
    var builder = MediaAsset.builder()
        .mediaId(mediaId)
        .assetId(getString(attrs, "assetId").orElse(null))
        .tenantId(getString(attrs, StorageConstants.DYNAMO_ATTR_TENANT_ID).orElse(null))
        .sourceAssetId(getString(attrs, "sourceAssetId").orElse(null))
        .type(getString(attrs, "type").map(AssetType::valueOf).orElse(null))
        .status(getString(attrs, "status").map(AssetStatus::valueOf).orElse(null))
        .outputFormat(getString(attrs, "outputFormat").orElse(null))
        .mimetype(getString(attrs, "mimetype").orElse(null))
        .size(getLong(attrs, "size").orElse(null))
        .width(getInt(attrs, "width").orElse(null))
        .height(getInt(attrs, "height").orElse(null))
        .downloadName(getString(attrs, "downloadName").orElse(null))
        .operation(getString(attrs, "operation").map(AssetOperation::fromString).orElse(null))
        .createdAt(getString(attrs, "createdAt").map(Instant::parse).orElse(null))
        .updatedAt(getString(attrs, "updatedAt").map(Instant::parse).orElse(null))
        .errorMessage(getString(attrs, "errorMessage").orElse(null));
    if (attrs.containsKey("tags") && attrs.get("tags").ss() != null) {
      builder.tags(List.copyOf(attrs.get("tags").ss()));
    }
    return Optional.of(builder.build());
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(long value) {
    return AttributeValue.builder().n(String.valueOf(value)).build();
  }

  private AttributeValue bool(boolean value) {
    return AttributeValue.builder().bool(value).build();
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
