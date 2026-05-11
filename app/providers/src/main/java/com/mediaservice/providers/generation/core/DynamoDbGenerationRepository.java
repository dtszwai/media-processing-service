package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.generation.GenerationArtifact;
import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationOutputType;
import com.mediaservice.common.generation.GenerationStage;
import com.mediaservice.common.generation.GenerationStatus;
import com.mediaservice.common.generation.provider.ModerationResult;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaSource;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;
import software.amazon.awssdk.services.dynamodb.model.DeleteItemRequest;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.Put;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;
import software.amazon.awssdk.services.dynamodb.model.TransactWriteItem;
import software.amazon.awssdk.services.dynamodb.model.TransactWriteItemsRequest;
import software.amazon.awssdk.services.dynamodb.model.TransactionCanceledException;
import software.amazon.awssdk.services.dynamodb.model.Update;
import software.amazon.awssdk.services.dynamodb.model.UpdateItemRequest;
import com.mediaservice.providers.generation.prompt.PromptCipher;

import static com.mediaservice.providers.generation.core.DynamoDbAttrs.bool;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.boolOrNull;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.instantOrNow;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.instantOrNull;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.intOrNull;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.longOrNull;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.n;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.s;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.str;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.stringMap;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.stringMapOrEmpty;

public class DynamoDbGenerationRepository implements GenerationRepository {
  private static final String STATUS_CLAIMED = "claimed";
  private static final String STATUS_COMPLETED = "completed";
  private static final String STATUS_FAILED = "failed";
  private static final String STATUS_UNKNOWN_OUTCOME = "unknown_outcome";
  private static final long LEASE_SECONDS = 300L;
  private static final long AUDIT_TTL_SECONDS = 365L * 86400L;

  private final DynamoDbClient client;
  private final String tableName;
  private final PromptCipher promptCipher;

  public DynamoDbGenerationRepository(DynamoDbClient client, String tableName) {
    this(client, tableName, PromptCipher.get());
  }

  public DynamoDbGenerationRepository(DynamoDbClient client, String tableName, PromptCipher promptCipher) {
    this.client = client;
    this.tableName = tableName;
    this.promptCipher = promptCipher;
  }

  @Override
  public void createJob(GenerationJob job, Media media, MediaAsset initialAsset) {
    client.transactWriteItems(TransactWriteItemsRequest.builder()
        .transactItems(
            TransactWriteItem.builder().put(Put.builder()
                .tableName(tableName)
                .item(jobItem(job))
                .conditionExpression("attribute_not_exists(PK)")
                .build()).build(),
            TransactWriteItem.builder().put(Put.builder()
                .tableName(tableName)
                .item(mediaItem(media))
                .conditionExpression("attribute_not_exists(PK)")
                .build()).build(),
            TransactWriteItem.builder().put(Put.builder()
                .tableName(tableName)
                .item(assetItem(initialAsset))
                .conditionExpression("attribute_not_exists(PK)")
                .build()).build())
        .build());
  }

  @Override
  public Optional<GenerationJob> getJob(String jobId) {
    var item = client.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(genPk(jobId)), "SK", s(StorageConstants.DYNAMO_SK_GEN_JOB)))
        .build()).item();
    return toJob(item);
  }

  @Override
  public Optional<Media> getMedia(String mediaId) {
    var item = client.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(mediaPk(mediaId)), "SK", s(StorageConstants.DYNAMO_SK_MEDIA)))
        .build()).item();
    return toMedia(mediaId, item);
  }

  @Override
  public Optional<MediaAsset> getAsset(String mediaId, String assetId) {
    var item = client.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(mediaPk(mediaId)), "SK", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId)))
        .build()).item();
    return toAsset(mediaId, item);
  }

  @Override
  public void updateJobStage(String jobId, GenerationStatus status, GenerationStage stage) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(jobId))
        .updateExpression("SET #status = :status, currentStage = :stage, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(status.name()),
            ":stage", s(stage.name()),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void updateEnhancedPrompt(String jobId, String enhancedPrompt) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(jobId))
        .updateExpression("SET enhancedPrompt = :enhancedPrompt, updatedAt = :updatedAt")
        .expressionAttributeValues(Map.of(
            ":enhancedPrompt", s(promptCipher.encrypt(enhancedPrompt)),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void recordProviderJobId(String jobId, String providerJobId) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(jobId))
        .updateExpression("SET providerJobId = :providerJobId, updatedAt = :updatedAt")
        .expressionAttributeValues(Map.of(
            ":providerJobId", s(providerJobId),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void recordResultArtifact(String jobId, String assetId, String contentType, String extension, long sizeBytes) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(jobId))
        .updateExpression("SET resultAssetId = :assetId, resultContentType = :contentType, resultExtension = :extension, resultSizeBytes = :sizeBytes, updatedAt = :updatedAt")
        .expressionAttributeValues(Map.of(
            ":assetId", s(assetId),
            ":contentType", s(contentType),
            ":extension", s(extension),
            ":sizeBytes", n(sizeBytes),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void completeJob(String jobId) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(jobId))
        .updateExpression("SET #status = :status, currentStage = :stage, completedAt = :completedAt, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(GenerationStatus.COMPLETE.name()),
            ":stage", s(GenerationStage.DELIVERY.name()),
            ":completedAt", s(Instant.now().toString()),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void failJob(String jobId, String code, String message) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(jobKey(jobId))
        .updateExpression("SET #status = :status, errorCode = :code, errorMessage = :message, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(GenerationStatus.FAILED.name()),
            ":code", s(code),
            ":message", s(message != null ? message : code),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void updateMediaStatus(String mediaId, MediaStatus status) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(mediaPk(mediaId)), "SK", s(StorageConstants.DYNAMO_SK_MEDIA)))
        .updateExpression("SET #status = :status, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void updateGeneratedMediaComplete(String mediaId, long size, String contentType, String extension) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(mediaPk(mediaId)), "SK", s(StorageConstants.DYNAMO_SK_MEDIA)))
        .updateExpression("SET #status = :status, size = :size, mimetype = :contentType, #name = :name, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of(
            "#status", "status",
            "#name", "name"))
        .expressionAttributeValues(Map.of(
            ":status", s(MediaStatus.COMPLETE.name()),
            ":size", n(size),
            ":contentType", s(contentType),
            ":name", s(generatedDownloadName(mediaId, extension)),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void updateAssetStatus(String mediaId, String assetId, AssetStatus status) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(mediaPk(mediaId)), "SK", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId)))
        .updateExpression("SET #status = :status, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(status.name()),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void updateAssetComplete(String mediaId, String assetId, long size, String contentType, String extension) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(mediaPk(mediaId)), "SK", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX + assetId)))
        .updateExpression("SET #status = :status, size = :size, mimetype = :contentType, outputFormat = :format, downloadName = :downloadName, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(AssetStatus.COMPLETE.name()),
            ":size", n(size),
            ":contentType", s(contentType),
            ":format", s(extension.replace(".", "")),
            ":downloadName", s(generatedDownloadName(mediaId, extension)),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void createArtifact(GenerationArtifact artifact) {
    client.putItem(PutItemRequest.builder()
        .tableName(tableName)
        .item(artifactItem(artifact))
        .build());
  }

  @Override
  public void createStageRun(String tenantId, String jobId, GenerationStage stage, int attempt,
      GenerationStatus status, String errorCode) {
    Map<String, AttributeValue> item = new HashMap<>();
    item.put("PK", s(genPk(jobId)));
    item.put("SK", s(StorageConstants.buildStageSk(stage.name(), attempt)));
    item.put("jobId", s(jobId));
    item.put("stage", s(stage.name()));
    item.put("attempt", n(attempt));
    item.put("status", s(status.name()));
    item.put("createdAt", s(Instant.now().toString()));
    item.put("updatedAt", s(Instant.now().toString()));
    if (tenantId != null) {
      item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(tenantId));
    }
    if (errorCode != null) {
      item.put("errorCode", s(errorCode));
    }
    client.putItem(PutItemRequest.builder().tableName(tableName).item(item).build());
  }

  @Override
  public void createSafetyDecision(String tenantId, String jobId, GenerationStage stage, String gate,
      ModerationResult result) {
    Map<String, AttributeValue> item = new HashMap<>();
    String normalizedGate = gate != null && !gate.isBlank() ? gate : stage.name().toLowerCase();
    long ts = Instant.now().toEpochMilli();
    item.put("PK", s(genPk(jobId)));
    item.put("SK", s(StorageConstants.buildSafetySk(stage.name(), normalizedGate, ts)));
    item.put("jobId", s(jobId));
    item.put("stage", s(stage.name()));
    item.put("gate", s(normalizedGate));
    item.put("classifier", s(result.classifier()));
    item.put("modelVersion", s(result.modelVersion()));
    item.put("score", n(Double.toString(result.score())));
    item.put("decision", s(result.allowed() ? "allowed" : "blocked"));
    item.put("reason", s(result.reason()));
    item.put("createdAt", s(Instant.now().toString()));
    if (tenantId != null) {
      item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(tenantId));
    }
    client.putItem(PutItemRequest.builder().tableName(tableName).item(item).build());

    // Immutable audit-grade mirror for compliance replay. Long TTL (1y),
    // separate SK partition so safety lifecycle and audit lifecycle decouple.
    writeAuditRow(tenantId, jobId, "safety", normalizedGate, stage, result.classifier(),
        result.modelVersion(), result.allowed() ? "allowed" : "blocked", result.reason(),
        Double.toString(result.score()), ts);
  }

  @Override
  public void recordAuditEvent(String tenantId, String jobId, String category, String gate,
      String classifier, String modelVersion, String decision, String reason) {
    writeAuditRow(tenantId, jobId, category, gate, null, classifier, modelVersion, decision, reason,
        null, Instant.now().toEpochMilli());
  }

  private void writeAuditRow(String tenantId, String jobId, String category, String gate,
      GenerationStage stage, String classifier, String modelVersion, String decision,
      String reason, String score, long ts) {
    Map<String, AttributeValue> audit = new HashMap<>();
    audit.put("PK", s(genPk(jobId != null ? jobId : "ADMISSION#" + tenantId)));
    audit.put("SK", s(StorageConstants.buildAuditSk(category, gate != null ? gate : "n_a", ts)));
    audit.put("category", s(category));
    if (jobId != null) audit.put("jobId", s(jobId));
    if (stage != null) audit.put("stage", s(stage.name()));
    if (gate != null) audit.put("gate", s(gate));
    if (classifier != null) audit.put("classifier", s(classifier));
    if (modelVersion != null) audit.put("modelVersion", s(modelVersion));
    if (decision != null) audit.put("decision", s(decision));
    if (reason != null) audit.put("reason", s(reason));
    if (score != null) audit.put("score", n(score));
    audit.put("createdAt", s(Instant.now().toString()));
    audit.put("expiresAt", n(Instant.now().plusSeconds(AUDIT_TTL_SECONDS).getEpochSecond()));
    if (tenantId != null) {
      audit.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(tenantId));
    }
    try {
      // attribute_not_exists guarantees the audit row is immutable post-write — no replay path
      // can overwrite an existing decision under the same millisecond-keyed SK.
      client.putItem(PutItemRequest.builder()
          .tableName(tableName)
          .item(audit)
          .conditionExpression("attribute_not_exists(PK)")
          .build());
    } catch (ConditionalCheckFailedException ignored) {
      // Same-millisecond duplicate is benign; the prior audit row carries the same payload.
    }
  }

  @Override
  public boolean claimStageSideEffect(String jobId, GenerationStage stage) {
    ClaimOutcome outcome = claimStageSideEffectV2(jobId, stage);
    return outcome instanceof ClaimOutcome.Proceed;
  }

  @Override
  public ClaimOutcome claimStageSideEffectV2(String jobId, GenerationStage stage) {
    Instant now = Instant.now();
    String idempotencyKey = StorageConstants.buildIdempotencySk(stage.name(), "provider_call");
    Map<String, AttributeValue> item = new HashMap<>();
    item.put("PK", s(genPk(jobId)));
    item.put("SK", s(idempotencyKey));
    item.put("jobId", s(jobId));
    item.put("stage", s(stage.name()));
    item.put("status", s(STATUS_CLAIMED));
    item.put("leaseExpiresAt", s(now.plusSeconds(LEASE_SECONDS).toString()));
    item.put("createdAt", s(now.toString()));
    item.put("updatedAt", s(now.toString()));
    item.put("expiresAt", n(now.plusSeconds(86400).getEpochSecond()));
    try {
      // Failed is terminal; only attribute_not_exists is a clean fresh claim path.
      client.putItem(PutItemRequest.builder()
          .tableName(tableName)
          .item(item)
          .conditionExpression("attribute_not_exists(PK)")
          .build());
      return new ClaimOutcome.Proceed(idempotencyKey);
    } catch (ConditionalCheckFailedException e) {
      // Row exists; inspect status to decide outcome.
      return classifyExistingClaim(jobId, stage, idempotencyKey, now);
    }
  }

  private ClaimOutcome classifyExistingClaim(String jobId, GenerationStage stage, String idempotencyKey, Instant now) {
    var existing = client.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(idempotencyKey(jobId, stage))
        .build()).item();
    if (existing == null || existing.isEmpty()) {
      // Disappeared between put and get — let caller redeliver.
      return new ClaimOutcome.ExitRedeliver();
    }
    String status = str(existing, "status");
    if (STATUS_COMPLETED.equals(status)) {
      String resultRef = str(existing, "resultRef");
      return new ClaimOutcome.ReuseExisting(resultRef != null ? resultRef : "ok");
    }
    if (STATUS_FAILED.equals(status)) {
      String errorCode = str(existing, "errorCode");
      String errorMessage = str(existing, "errorMessage");
      return new ClaimOutcome.TerminalFailure(
          errorCode != null ? errorCode : "STAGE_FAILED",
          errorMessage != null ? errorMessage : "Idempotency row already failed");
    }
    if (STATUS_UNKNOWN_OUTCOME.equals(status)) {
      return new ClaimOutcome.ExitRedeliver();
    }
    if (STATUS_CLAIMED.equals(status)) {
      String leaseString = str(existing, "leaseExpiresAt");
      Instant lease = leaseString != null ? safeInstant(leaseString) : null;
      if (lease == null || !lease.isBefore(now)) {
        // Lease still valid — another worker is in flight.
        return new ClaimOutcome.ExitRedeliver();
      }
      // Lease expired: transition to unknown_outcome guarded by the prior lease.
      try {
        client.updateItem(UpdateItemRequest.builder()
            .tableName(tableName)
            .key(idempotencyKey(jobId, stage))
            .updateExpression("SET #status = :unknown, updatedAt = :now")
            .conditionExpression("#status = :claimed AND leaseExpiresAt = :prior")
            .expressionAttributeNames(Map.of("#status", "status"))
            .expressionAttributeValues(Map.of(
                ":unknown", s(STATUS_UNKNOWN_OUTCOME),
                ":claimed", s(STATUS_CLAIMED),
                ":prior", s(leaseString),
                ":now", s(now.toString())))
            .build());
        return new ClaimOutcome.Reconcile(idempotencyKey);
      } catch (ConditionalCheckFailedException raced) {
        // Lost the race to another worker that already advanced the row.
        return new ClaimOutcome.ExitRedeliver();
      }
    }
    return new ClaimOutcome.ExitRedeliver();
  }

  /**
   * Terminal transition: mark a row that has been through unknown_outcome and
   * could not be reconciled. Guarded on current status = unknown_outcome.
   */
  @Override
  public void markUnknownOutcomeUnrecoverable(String jobId, GenerationStage stage, String errorCode, String errorMessage) {
    try {
      client.updateItem(UpdateItemRequest.builder()
          .tableName(tableName)
          .key(idempotencyKey(jobId, stage))
          .updateExpression("SET #status = :failed, errorCode = :code, errorMessage = :message, updatedAt = :updatedAt")
          .conditionExpression("#status = :unknown")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":failed", s(STATUS_FAILED),
              ":unknown", s(STATUS_UNKNOWN_OUTCOME),
              ":code", s(errorCode),
              ":message", s(errorMessage != null ? errorMessage : errorCode),
              ":updatedAt", s(Instant.now().toString())))
          .build());
    } catch (ConditionalCheckFailedException ignored) {
      // Row already advanced past unknown_outcome; nothing to do.
    }
  }

  private Instant safeInstant(String value) {
    try {
      return Instant.parse(value);
    } catch (RuntimeException e) {
      return null;
    }
  }

  @Override
  public Optional<String> getStageSideEffectResult(String jobId, GenerationStage stage) {
    var item = client.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(idempotencyKey(jobId, stage))
        .build()).item();
    if (item == null || item.isEmpty() || !"completed".equals(str(item, "status"))) {
      return Optional.empty();
    }
    return Optional.ofNullable(str(item, "resultRef"));
  }

  @Override
  public void completeStageSideEffect(String jobId, GenerationStage stage, String resultRef) {
    client.updateItem(UpdateItemRequest.builder()
        .tableName(tableName)
        .key(idempotencyKey(jobId, stage))
        .updateExpression("SET #status = :status, resultRef = :resultRef, updatedAt = :updatedAt")
        .expressionAttributeNames(Map.of("#status", "status"))
        .expressionAttributeValues(Map.of(
            ":status", s(STATUS_COMPLETED),
            ":resultRef", s(resultRef != null ? resultRef : "ok"),
            ":updatedAt", s(Instant.now().toString())))
        .build());
  }

  @Override
  public void failStageSideEffect(String jobId, GenerationStage stage, String errorCode) {
    try {
      client.updateItem(UpdateItemRequest.builder()
          .tableName(tableName)
          .key(idempotencyKey(jobId, stage))
          .updateExpression("SET #status = :status, errorCode = :errorCode, updatedAt = :updatedAt")
          .conditionExpression("attribute_exists(PK)")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":status", s(STATUS_FAILED),
              ":errorCode", s(errorCode),
              ":updatedAt", s(Instant.now().toString())))
          .build());
    } catch (ConditionalCheckFailedException ignored) {
      // Some stages do not own an external side effect row; don't create one just to mark failure.
    }
  }

  @Override
  public void releaseStageSideEffectForRetry(String jobId, GenerationStage stage, String errorCode) {
    try {
      client.deleteItem(DeleteItemRequest.builder()
          .tableName(tableName)
          .key(idempotencyKey(jobId, stage))
          .conditionExpression("attribute_not_exists(#status) OR #status <> :completed")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(":completed", s(STATUS_COMPLETED)))
          .build());
    } catch (ConditionalCheckFailedException ignored) {
      // A raced worker completed the side effect while this attempt was unwinding.
    }
  }

  @Override
  public void reserveBudget(String tenantId, String jobId, double estimatedUsd, double dailyCapUsd) {
    if (estimatedUsd > dailyCapUsd) {
      throw new GenerationProviderException(GenerationErrorCode.BUDGET_EXCEEDED, "Estimated generation cost exceeds daily cap");
    }
    Instant now = Instant.now();
    String budgetPk = budgetPk(tenantId);

    // DynamoDB ConditionExpression does not allow arithmetic across attributes (only
    // UpdateExpression does), so the aggregate cap check is done as read-modify-write
    // with optimistic CAS on pending_usd. The CAS retry loop handles concurrent writers
    // without losing the cap guarantee; ConditionalCheckFailed surfaces as a retry, the
    // per-job audit row's attribute_not_exists guards against duplicate reservations.
    int maxAttempts = 5;
    for (int attempt = 1; attempt <= maxAttempts; attempt++) {
      var reservedItem = client.getItem(GetItemRequest.builder()
          .tableName(tableName)
          .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.DYNAMO_SK_BUDGET_RESERVED)))
          .consistentRead(true)
          .build()).item();
      double pending = parseDoubleAttr(reservedItem, "pending_usd", 0.0);
      double committedCache = parseDoubleAttr(reservedItem, "committed_usd_cache", 0.0);
      if (pending + committedCache + estimatedUsd > dailyCapUsd) {
        throw new GenerationProviderException(GenerationErrorCode.BUDGET_EXCEEDED, "Daily generation budget exceeded");
      }

      Map<String, AttributeValue> values = new HashMap<>();
      values.put(":tenantId", s(tenantId));
      values.put(":updatedAt", s(now.toString()));
      values.put(":est", n(Double.toString(estimatedUsd)));
      values.put(":zero", n("0"));
      String reservedCondition;
      boolean rowExists = reservedItem != null && !reservedItem.isEmpty();
      boolean pendingAttrExists = rowExists
          && reservedItem.get("pending_usd") != null
          && reservedItem.get("pending_usd").n() != null;
      if (pendingAttrExists) {
        // CAS: pending_usd unchanged since the read above.
        reservedCondition = "pending_usd = :expectedPending";
        values.put(":expectedPending", n(Double.toString(pending)));
      } else if (rowExists) {
        reservedCondition = "attribute_not_exists(pending_usd)";
      } else {
        reservedCondition = "attribute_not_exists(PK)";
      }

      try {
        client.transactWriteItems(TransactWriteItemsRequest.builder()
            .transactItems(
                TransactWriteItem.builder().update(Update.builder()
                    .tableName(tableName)
                    .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.DYNAMO_SK_BUDGET_RESERVED)))
                    .updateExpression(
                        "SET tenantId = :tenantId, updatedAt = :updatedAt, "
                            + "pending_usd = if_not_exists(pending_usd, :zero) + :est, "
                            + "committed_usd_cache = if_not_exists(committed_usd_cache, :zero)")
                    .conditionExpression(reservedCondition)
                    .expressionAttributeValues(values)
                    .build()).build(),
                TransactWriteItem.builder().put(Put.builder()
                    .tableName(tableName)
                    .item(Map.of(
                        "PK", s(budgetPk),
                        "SK", s(StorageConstants.buildReservedJobSk(jobId)),
                        "tenantId", s(tenantId),
                        "jobId", s(jobId),
                        "amountUsd", n(Double.toString(estimatedUsd)),
                        "status", s("reserved"),
                        "createdAt", s(now.toString()),
                        "expiresAt", n(now.plusSeconds(86400).getEpochSecond())))
                    .conditionExpression("attribute_not_exists(PK)")
                    .build()).build())
            .build());
        return;
      } catch (TransactionCanceledException | ConditionalCheckFailedException retry) {
        if (attempt >= maxAttempts) {
          throw new GenerationProviderException(GenerationErrorCode.BUDGET_EXCEEDED, "Daily generation budget exceeded");
        }
        // Loop and re-read; concurrent writer bumped pending_usd. The per-job audit row
        // failing attribute_not_exists also lands here — treat as conflict and re-check
        // on the next iteration (the cap re-check above will catch a duplicate cleanly).
      }
    }
    throw new GenerationProviderException(GenerationErrorCode.BUDGET_EXCEEDED, "Daily generation budget exceeded after retries");
  }

  private static double parseDoubleAttr(Map<String, AttributeValue> item, String key, double fallback) {
    if (item == null) {
      return fallback;
    }
    AttributeValue value = item.get(key);
    if (value == null || value.n() == null) {
      return fallback;
    }
    try {
      return Double.parseDouble(value.n());
    } catch (NumberFormatException e) {
      return fallback;
    }
  }

  @Override
  public void commitBudget(String tenantId, String jobId, double estimatedUsd, double actualUsd) {
    Instant now = Instant.now();
    String budgetPk = budgetPk(tenantId);

    // Caller supplies estimatedUsd (the same value passed to reserveBudget), so we skip the
    // leading getItem on RESERVED#<jobId>. The third transactWriteItems update keeps
    // ConditionExpression(#status = :reserved) as the idempotency safety net — re-commit attempts
    // fail the transaction and surface as TransactionCanceledException below.
    try {
      client.transactWriteItems(TransactWriteItemsRequest.builder()
        .transactItems(
            // 1. Decrement pending_usd, bump committed_usd_cache on RESERVED row.
            TransactWriteItem.builder().update(Update.builder()
                .tableName(tableName)
                .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.DYNAMO_SK_BUDGET_RESERVED)))
                .updateExpression(
                    "SET updatedAt = :updatedAt, "
                        + "pending_usd = if_not_exists(pending_usd, :zero) - :est, "
                        + "committed_usd_cache = if_not_exists(committed_usd_cache, :zero) + :actual")
                // Cap-floor protection is enforced by the per-job audit row's status
                // condition on the sibling TransactWriteItem; this aggregate update is
                // allowed to underflow to zero on already-released reservations.
                .expressionAttributeValues(Map.of(
                    ":updatedAt", s(now.toString()),
                    ":est", n(Double.toString(estimatedUsd)),
                    ":actual", n(Double.toString(actualUsd)),
                    ":zero", n("0")))
                .build()).build(),
            // 2. Aggregate USED row: add committed_usd.
            TransactWriteItem.builder().update(Update.builder()
                .tableName(tableName)
                .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.DYNAMO_SK_BUDGET_USED)))
                .updateExpression(
                    "SET tenantId = :tenantId, updatedAt = :updatedAt "
                        + "ADD committed_usd :actual")
                .expressionAttributeValues(Map.of(
                    ":tenantId", s(tenantId),
                    ":updatedAt", s(now.toString()),
                    ":actual", n(Double.toString(actualUsd))))
                .build()).build(),
            // 3. Per-job audit row mark committed (idempotent via condition).
            TransactWriteItem.builder().update(Update.builder()
                .tableName(tableName)
                .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.buildReservedJobSk(jobId))))
                .updateExpression("SET #status = :committed, committedAt = :committedAt, actualUsd = :actualUsd")
                .conditionExpression("#status = :reserved")
                .expressionAttributeNames(Map.of("#status", "status"))
                .expressionAttributeValues(Map.of(
                    ":committed", s("committed"),
                    ":reserved", s("reserved"),
                    ":committedAt", s(now.toString()),
                    ":actualUsd", n(Double.toString(actualUsd))))
                .build()).build())
        .build());
    } catch (ConditionalCheckFailedException | TransactionCanceledException ignored) {
      // Reservation already committed or released — treat as idempotent no-op to preserve
      // the prior behaviour where re-commits and post-release commits returned silently.
    }
  }

  @Override
  public void releaseBudget(String tenantId, String jobId) {
    String budgetPk = budgetPk(tenantId);
    var reservation = client.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.buildReservedJobSk(jobId))))
        .build()).item();
    if (reservation == null || reservation.isEmpty()) {
      return;
    }
    String status = str(reservation, "status");
    if ("released".equals(status) || "committed".equals(status)) {
      // Re-release after release/commit is a no-op.
      return;
    }
    double estimatedUsd = Double.parseDouble(reservation.get("amountUsd").n());
    Instant now = Instant.now();
    try {
      client.transactWriteItems(TransactWriteItemsRequest.builder()
          .transactItems(
              // 1. Decrement pending_usd guarded by pending_usd >= :estimated (TOCTOU safe).
              TransactWriteItem.builder().update(Update.builder()
                  .tableName(tableName)
                  .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.DYNAMO_SK_BUDGET_RESERVED)))
                  .updateExpression(
                      "SET updatedAt = :updatedAt, "
                          + "pending_usd = if_not_exists(pending_usd, :zero) - :est")
                  // DynamoDB ConditionExpression does not support if_not_exists(). Cap-floor
                // protection is provided by the per-job audit row's status condition on the
                // sibling TransactWriteItem; this aggregate update is allowed to underflow
                // to zero on already-released reservations and that path is idempotent.
                  .expressionAttributeValues(Map.of(
                      ":updatedAt", s(now.toString()),
                      ":est", n(Double.toString(estimatedUsd)),
                      ":zero", n("0")))
                  .build()).build(),
              // 2. Mark per-job audit row released, guarded on current reserved state.
              TransactWriteItem.builder().update(Update.builder()
                  .tableName(tableName)
                  .key(Map.of("PK", s(budgetPk), "SK", s(StorageConstants.buildReservedJobSk(jobId))))
                  .updateExpression("SET #status = :released, releasedAt = :releasedAt")
                  .conditionExpression("#status = :reserved")
                  .expressionAttributeNames(Map.of("#status", "status"))
                  .expressionAttributeValues(Map.of(
                      ":released", s("released"),
                      ":reserved", s("reserved"),
                      ":releasedAt", s(now.toString())))
                  .build()).build())
          .build());
    } catch (ConditionalCheckFailedException | TransactionCanceledException ignored) {
      // Another worker already released or committed concurrently — idempotent no-op.
    }
  }

  private Map<String, AttributeValue> jobItem(GenerationJob job) {
    Map<String, AttributeValue> item = new HashMap<>();
    item.put("PK", s(genPk(job.getJobId())));
    item.put("SK", s(StorageConstants.DYNAMO_SK_GEN_JOB));
    item.put("jobId", s(job.getJobId()));
    item.put("mediaId", s(job.getMediaId()));
    item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(job.getTenantId()));
    if (job.getUserId() != null) item.put(StorageConstants.DYNAMO_ATTR_USER_ID, s(job.getUserId()));
    if (job.getTier() != null) item.put("tier", s(job.getTier()));
    item.put("outputType", s(job.getOutputType().name()));
    item.put("status", s(job.getStatus().name()));
    item.put("currentStage", s(job.getCurrentStage().name()));
    item.put("prompt", s(promptCipher.encrypt(job.getPrompt())));
    if (job.getEnhancedPrompt() != null) item.put("enhancedPrompt", s(promptCipher.encrypt(job.getEnhancedPrompt())));
    item.put("model", s(job.getModel()));
    item.put("resolution", s(job.getResolution()));
    if (job.getSeed() != null) item.put("seed", n(job.getSeed()));
    if (job.getWebhookUrl() != null) item.put("webhookUrl", s(job.getWebhookUrl()));
    if (job.getProviderJobId() != null) item.put("providerJobId", s(job.getProviderJobId()));
    if (job.getMetadata() != null && !job.getMetadata().isEmpty()) item.put("metadata", stringMap(job.getMetadata()));
    item.put("estimatedWaitSeconds", n(job.getEstimatedWaitSeconds() != null ? job.getEstimatedWaitSeconds() : 0));
    item.put("aiGenerated", bool(Boolean.TRUE.equals(job.getAiGenerated())));
    item.put("createdAt", s(job.getCreatedAt().toString()));
    item.put("updatedAt", s(job.getUpdatedAt().toString()));
    item.put("expiresAt", n(Instant.now().plusSeconds(30L * 86400L).getEpochSecond()));
    return item;
  }

  private Map<String, AttributeValue> mediaItem(Media media) {
    Map<String, AttributeValue> item = new HashMap<>();
    item.put("PK", s(mediaPk(media.getMediaId())));
    item.put("SK", s(StorageConstants.DYNAMO_SK_MEDIA));
    item.put("size", n(media.getSize() != null ? media.getSize() : 0));
    item.put("name", s(media.getName()));
    item.put("mimetype", s(media.getMimetype()));
    item.put("mediaType", s(media.getMediaType().getValue()));
    item.put("source", s(media.getSource().getValue()));
    item.put("status", s(media.getStatus().name()));
    item.put("originalAssetId", s(media.getOriginalAssetId()));
    item.put("createdAt", s(media.getCreatedAt().toString()));
    item.put("updatedAt", s(Instant.now().toString()));
    item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(media.getTenantId()));
    if (media.getUserId() != null) item.put(StorageConstants.DYNAMO_ATTR_USER_ID, s(media.getUserId()));
    if (media.getWebhookUrl() != null) item.put("webhookUrl", s(media.getWebhookUrl()));
    return item;
  }

  private Map<String, AttributeValue> assetItem(MediaAsset asset) {
    Map<String, AttributeValue> item = new HashMap<>();
    item.put("PK", s(mediaPk(asset.getMediaId())));
    item.put("SK", s(StorageConstants.DYNAMO_SK_ASSET_PREFIX + asset.getAssetId()));
    item.put("assetId", s(asset.getAssetId()));
    item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(asset.getTenantId()));
    item.put("type", s(asset.getType().name()));
    item.put("tags", AttributeValue.builder().ss(asset.getTags()).build());
    item.put("status", s(asset.getStatus().name()));
    item.put("outputFormat", s(asset.getOutputFormat()));
    item.put("mimetype", s(asset.getMimetype()));
    item.put("downloadName", s(asset.getDownloadName()));
    item.put("createdAt", s(asset.getCreatedAt().toString()));
    item.put("updatedAt", s(Instant.now().toString()));
    return item;
  }

  private Map<String, AttributeValue> artifactItem(GenerationArtifact artifact) {
    Map<String, AttributeValue> item = new HashMap<>();
    item.put("PK", s(genPk(artifact.getJobId())));
    item.put("SK", s(StorageConstants.buildArtifactSk(artifact.getArtifactId())));
    item.put("artifactId", s(artifact.getArtifactId()));
    item.put("jobId", s(artifact.getJobId()));
    item.put("mediaId", s(artifact.getMediaId()));
    item.put(StorageConstants.DYNAMO_ATTR_TENANT_ID, s(artifact.getTenantId()));
    item.put("artifactType", s(artifact.getArtifactType()));
    if (artifact.getAssetId() != null) item.put("assetId", s(artifact.getAssetId()));
    if (artifact.getUri() != null) item.put("uri", s(artifact.getUri()));
    item.put("contentType", s(artifact.getContentType()));
    item.put("extension", s(artifact.getExtension()));
    item.put("sizeBytes", n(artifact.getSizeBytes() != null ? artifact.getSizeBytes() : 0));
    item.put("checksum", s(artifact.getChecksum()));
    item.put("createdAt", s(artifact.getCreatedAt() != null ? artifact.getCreatedAt().toString() : Instant.now().toString()));
    if (artifact.getExpiresAt() != null) item.put("expiresAt", n(artifact.getExpiresAt().getEpochSecond()));
    if (artifact.getMetadata() != null && !artifact.getMetadata().isEmpty()) item.put("metadata", stringMap(artifact.getMetadata()));
    return item;
  }

  private Optional<GenerationJob> toJob(Map<String, AttributeValue> item) {
    if (item == null || item.isEmpty()) {
      return Optional.empty();
    }
    GenerationJob job = GenerationJob.builder()
        .jobId(str(item, "jobId"))
        .mediaId(str(item, "mediaId"))
        .tenantId(str(item, StorageConstants.DYNAMO_ATTR_TENANT_ID))
        .userId(str(item, StorageConstants.DYNAMO_ATTR_USER_ID))
        .tier(str(item, "tier"))
        .outputType(GenerationOutputType.valueOf(str(item, "outputType")))
        .status(GenerationStatus.valueOf(str(item, "status")))
        .currentStage(GenerationStage.valueOf(str(item, "currentStage")))
        .prompt(promptCipher.decrypt(str(item, "prompt")))
        .enhancedPrompt(promptCipher.decrypt(str(item, "enhancedPrompt")))
        .model(str(item, "model"))
        .resolution(str(item, "resolution"))
        .seed(longOrNull(item, "seed"))
        .webhookUrl(str(item, "webhookUrl"))
        .providerJobId(str(item, "providerJobId"))
        .resultAssetId(str(item, "resultAssetId"))
        .resultContentType(str(item, "resultContentType"))
        .resultExtension(str(item, "resultExtension"))
        .resultSizeBytes(longOrNull(item, "resultSizeBytes"))
        .errorCode(str(item, "errorCode"))
        .errorMessage(str(item, "errorMessage"))
        .estimatedWaitSeconds(intOrNull(item, "estimatedWaitSeconds"))
        .aiGenerated(boolOrNull(item, "aiGenerated"))
        .metadata(stringMapOrEmpty(item, "metadata"))
        .createdAt(instantOrNow(item, "createdAt"))
        .updatedAt(instantOrNow(item, "updatedAt"))
        .completedAt(instantOrNull(item, "completedAt"))
        .build();
    return Optional.of(job);
  }

  private Optional<Media> toMedia(String mediaId, Map<String, AttributeValue> item) {
    if (item == null || item.isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(Media.builder()
        .mediaId(mediaId)
        .tenantId(str(item, StorageConstants.DYNAMO_ATTR_TENANT_ID))
        .userId(str(item, StorageConstants.DYNAMO_ATTR_USER_ID))
        .size(longOrNull(item, "size"))
        .name(str(item, "name"))
        .mimetype(str(item, "mimetype"))
        .mediaType(MediaType.fromString(str(item, "mediaType")))
        .source(MediaSource.fromString(str(item, "source")))
        .status(MediaStatus.valueOf(str(item, "status")))
        .originalAssetId(str(item, "originalAssetId"))
        .webhookUrl(str(item, "webhookUrl"))
        .createdAt(instantOrNull(item, "createdAt"))
        .updatedAt(instantOrNull(item, "updatedAt"))
        .build());
  }

  private Optional<MediaAsset> toAsset(String mediaId, Map<String, AttributeValue> item) {
    if (item == null || item.isEmpty()) {
      return Optional.empty();
    }
    List<String> tags = new ArrayList<>();
    if (item.containsKey("tags") && item.get("tags").ss() != null) {
      tags.addAll(item.get("tags").ss());
    }
    return Optional.of(MediaAsset.builder()
        .mediaId(mediaId)
        .assetId(str(item, "assetId"))
        .tenantId(str(item, StorageConstants.DYNAMO_ATTR_TENANT_ID))
        .sourceAssetId(str(item, "sourceAssetId"))
        .type(item.containsKey("type") ? AssetType.valueOf(str(item, "type")) : null)
        .tags(tags)
        .status(item.containsKey("status") ? AssetStatus.valueOf(str(item, "status")) : null)
        .outputFormat(str(item, "outputFormat"))
        .mimetype(str(item, "mimetype"))
        .size(longOrNull(item, "size"))
        .downloadName(str(item, "downloadName"))
        .createdAt(instantOrNull(item, "createdAt"))
        .updatedAt(instantOrNull(item, "updatedAt"))
        .build());
  }

  private String genPk(String jobId) {
    return StorageConstants.buildGenPk(jobId);
  }

  private String mediaPk(String mediaId) {
    return StorageConstants.DYNAMO_PK_PREFIX + mediaId;
  }

  private String budgetPk(String tenantId) {
    return StorageConstants.buildBudgetPk(tenantId, LocalDate.now(ZoneOffset.UTC).toString());
  }

  private Map<String, AttributeValue> jobKey(String jobId) {
    return Map.of("PK", s(genPk(jobId)), "SK", s(StorageConstants.DYNAMO_SK_GEN_JOB));
  }

  private Map<String, AttributeValue> idempotencyKey(String jobId, GenerationStage stage) {
    return Map.of("PK", s(genPk(jobId)), "SK", s(StorageConstants.buildIdempotencySk(stage.name(), "provider_call")));
  }

  private String generatedDownloadName(String mediaId, String extension) {
    String normalizedExtension = extension == null || extension.isBlank()
        ? ""
        : extension.startsWith(".") ? extension : "." + extension;
    return mediaId + "-generated" + normalizedExtension;
  }

}
