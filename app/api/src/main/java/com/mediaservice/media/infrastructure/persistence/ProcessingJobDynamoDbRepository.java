package com.mediaservice.media.infrastructure.persistence;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.ProcessingJob;
import com.mediaservice.common.model.ProcessingJobStatus;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import java.time.Instant;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;

@Slf4j
@Repository
public class ProcessingJobDynamoDbRepository extends AbstractDynamoDbRepository<ProcessingJob> {
  public ProcessingJobDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  @Override
  public ProcessingJob mapFromItem(Map<String, AttributeValue> item) {
    var pk = getString(item, "PK");
    var mediaId = pk.replace(StorageConstants.DYNAMO_PK_PREFIX, "");
    return ProcessingJob.builder()
        .jobId(getString(item, "jobId"))
        .mediaId(mediaId)
        .tenantId(getString(item, StorageConstants.DYNAMO_ATTR_TENANT_ID))
        .assetId(getString(item, "assetId"))
        .sourceAssetId(getString(item, "sourceAssetId"))
        .operation(AssetOperation.fromString(getString(item, "operation")))
        .outputFormat(getString(item, "outputFormat"))
        .width(getInt(item, "width"))
        .downloadName(getString(item, "downloadName"))
        .tags(getStringList(item, "tags"))
        .status(parseStatus(getString(item, "status")))
        .attempts(getInt(item, "attempts"))
        .errorMessage(getString(item, "errorMessage"))
        .createdAt(getInstant(item, "createdAt"))
        .updatedAt(getInstant(item, "updatedAt"))
        .build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(ProcessingJob job) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(StorageConstants.DYNAMO_PK_PREFIX + job.getMediaId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_JOB_PREFIX + job.getJobId()));
    item.put("jobId", s(job.getJobId()));
    if (job.getTenantId() != null) {
      item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(job.getTenantId()));
    }
    if (job.getAssetId() != null) {
      item.put("assetId", s(job.getAssetId()));
    }
    if (job.getSourceAssetId() != null) {
      item.put("sourceAssetId", s(job.getSourceAssetId()));
    }
    if (job.getOperation() != null) {
      item.put("operation", s(job.getOperation().getValue()));
    }
    if (job.getOutputFormat() != null) {
      item.put("outputFormat", s(job.getOutputFormat()));
    }
    if (job.getWidth() != null) {
      item.put("width", n(String.valueOf(job.getWidth())));
    }
    if (job.getDownloadName() != null) {
      item.put("downloadName", s(job.getDownloadName()));
    }
    if (job.getTags() != null && !job.getTags().isEmpty()) {
      item.put("tags", AttributeValue.builder().ss(job.getTags()).build());
    }
    item.put("status", s(job.getStatus() != null ? job.getStatus().name() : ProcessingJobStatus.PENDING.name()));
    if (job.getAttempts() != null) {
      item.put("attempts", n(String.valueOf(job.getAttempts())));
    }
    if (job.getErrorMessage() != null) {
      item.put("errorMessage", s(job.getErrorMessage()));
    }
    item.put("createdAt", s(job.getCreatedAt() != null ? job.getCreatedAt().toString() : now.toString()));
    item.put("updatedAt", s(now.toString()));
    return item;
  }

  public void createJob(ProcessingJob job) {
    save(job);
    log.info("Created processing job jobId={} for mediaId={}", job.getJobId(), job.getMediaId());
  }

  public Optional<ProcessingJob> getJob(String mediaId, String jobId) {
    return findByKey(StorageConstants.DYNAMO_PK_PREFIX + mediaId, StorageConstants.DYNAMO_SK_JOB_PREFIX + jobId);
  }

  public boolean updateStatusConditionally(String mediaId, String jobId, ProcessingJobStatus newStatus, ProcessingJobStatus expectedStatus) {
    return updateConditionally(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_JOB_PREFIX + jobId,
        "SET #status = :newStatus, updatedAt = :updatedAt",
        "#status = :expectedStatus",
        Map.of("#status", "status"),
        Map.of(
            ":newStatus", s(newStatus.name()),
            ":expectedStatus", s(expectedStatus.name()),
            ":updatedAt", s(Instant.now().toString())));
  }

  public void updateJobError(String mediaId, String jobId, String errorMessage) {
    updateAttributes(
        StorageConstants.DYNAMO_PK_PREFIX + mediaId,
        StorageConstants.DYNAMO_SK_JOB_PREFIX + jobId,
        "SET #status = :status, errorMessage = :errorMessage, updatedAt = :updatedAt",
        Map.of("#status", "status"),
        Map.of(
            ":status", s(ProcessingJobStatus.ERROR.name()),
            ":errorMessage", s(errorMessage),
            ":updatedAt", s(Instant.now().toString())));
  }

  private ProcessingJobStatus parseStatus(String value) {
    return value != null ? ProcessingJobStatus.valueOf(value) : null;
  }

  private List<String> getStringList(Map<String, AttributeValue> item, String key) {
    if (!item.containsKey(key)) {
      return null;
    }
    var attr = item.get(key);
    return attr.ss() != null ? List.copyOf(attr.ss()) : null;
  }
}
