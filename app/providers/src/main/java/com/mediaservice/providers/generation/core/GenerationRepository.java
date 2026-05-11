package com.mediaservice.providers.generation.core;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.generation.GenerationArtifact;
import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationStage;
import com.mediaservice.common.generation.GenerationStatus;
import com.mediaservice.common.generation.provider.ModerationResult;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaStatus;
import java.util.Optional;

public interface GenerationRepository {
  void createJob(GenerationJob job, Media media, MediaAsset initialAsset);

  Optional<GenerationJob> getJob(String jobId);

  Optional<Media> getMedia(String mediaId);

  Optional<MediaAsset> getAsset(String mediaId, String assetId);

  void updateJobStage(String jobId, GenerationStatus status, GenerationStage stage);

  void updateEnhancedPrompt(String jobId, String enhancedPrompt);

  void recordProviderJobId(String jobId, String providerJobId);

  void recordResultArtifact(String jobId, String assetId, String contentType, String extension, long sizeBytes);

  void completeJob(String jobId);

  void failJob(String jobId, String code, String message);

  void updateMediaStatus(String mediaId, MediaStatus status);

  default void updateGeneratedMediaComplete(
      String mediaId,
      long size,
      String contentType,
      String extension) {
    updateMediaStatus(mediaId, MediaStatus.COMPLETE);
  }

  void updateAssetComplete(String mediaId, String assetId, long size, String contentType, String extension);

  /**
   * Terminal asset-status transition (e.g. ERROR) for the placeholder source asset created at
   * job submission. Without this the asset row stays PENDING after the parent media flips to
   * ERROR, leaving the UI gallery card and the parent row inconsistent. Test doubles may no-op
   * if they do not surface per-asset status in assertions.
   */
  default void updateAssetStatus(String mediaId, String assetId, AssetStatus status) {}

  void createArtifact(GenerationArtifact artifact);

  void createStageRun(String tenantId, String jobId, GenerationStage stage, int attempt,
      GenerationStatus status, String errorCode);

  void createSafetyDecision(String tenantId, String jobId, GenerationStage stage, String gate,
      ModerationResult result);

  /**
   * Immutable audit-grade event for compliance replay. The DynamoDB
   * implementation writes a long-TTL AUDIT# row that cannot be overwritten. Test doubles
   * and in-memory fakes can no-op — audit is a side-channel, never read by the workflow.
   */
  default void recordAuditEvent(String tenantId, String jobId, String category, String gate,
      String classifier, String modelVersion, String decision, String reason) {}

  boolean claimStageSideEffect(String jobId, GenerationStage stage);

  /**
   * Five-state idempotency claim. Returns one of the outcomes defined in
   * {@link ClaimOutcome} — Proceed, ReuseExisting, Reconcile, ExitRedeliver,
   * or TerminalFailure. The default falls back to the legacy boolean form for
   * test doubles that have not been migrated.
   */
  default ClaimOutcome claimStageSideEffectV2(String jobId, GenerationStage stage) {
    if (claimStageSideEffect(jobId, stage)) {
      return new ClaimOutcome.Proceed(StorageConstants.buildIdempotencySk(stage.name(), "provider_call"));
    }
    Optional<String> existing = getStageSideEffectResult(jobId, stage);
    return existing.<ClaimOutcome>map(ClaimOutcome.ReuseExisting::new)
        .orElseGet(ClaimOutcome.ExitRedeliver::new);
  }

  Optional<String> getStageSideEffectResult(String jobId, GenerationStage stage);

  void completeStageSideEffect(String jobId, GenerationStage stage, String resultRef);

  void failStageSideEffect(String jobId, GenerationStage stage, String errorCode);

  /**
   * Release a stage idempotency claim after a retryable failure so the next attempt can acquire
   * the side-effect slot again. Implementations that do not distinguish retry release from
   * terminal failure may fall back to their legacy failure behavior.
   */
  default void releaseStageSideEffectForRetry(String jobId, GenerationStage stage, String errorCode) {
    failStageSideEffect(jobId, stage, errorCode);
  }

  void reserveBudget(String tenantId, String jobId, double estimatedUsd, double dailyCapUsd);

  /**
   * Commit a previously reserved budget. {@code estimatedUsd} is the same amount that was
   * passed to {@link #reserveBudget(String, String, double, double)} — the implementation
   * may use it directly to decrement {@code pending_usd} without an extra read of the
   * reservation row. The reservation status condition still guards idempotency.
   */
  void commitBudget(String tenantId, String jobId, double estimatedUsd, double actualUsd);

  void releaseBudget(String tenantId, String jobId);

  /**
   * Terminal transition for an idempotency row that has been stuck in {@code unknown_outcome}
   * and could not be reconciled. The DynamoDB implementation flips the row to {@code failed}
   * guarded on the current state being {@code unknown_outcome}. Test doubles and in-memory
   * fakes can no-op this — surfacing the error code through normal failJob is sufficient there.
   */
  default void markUnknownOutcomeUnrecoverable(String jobId, GenerationStage stage,
      String errorCode, String errorMessage) {}
}
