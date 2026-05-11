package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationStage;
import java.time.Duration;

public interface GenerationMetrics {
  void recordStageLatency(GenerationJob job, GenerationStage stage, Duration duration, boolean success, String errorCode);

  void recordArtifactCost(GenerationJob job, GenerationStage stage, String provider, double usd);

  void recordBudgetUsage(GenerationJob job, double reservedUsd, double dailyCapUsd);

  void recordBudgetCommitted(GenerationJob job, double usedUsd);

  void recordAdmissionVerdict(String tenantId, String tier, String decision, String reason);

  void recordAdmissionRejected(String tenantId, String reason);

  void recordSafetyDecision(GenerationJob job, GenerationStage stage, String gate, boolean allowed, String reason,
      String classifier);

  void recordWatermarkVerification(GenerationJob job, boolean present);

  void recordStageRetry(GenerationJob job, GenerationStage stage, int nextAttempt, String errorCode);

  default void recordIdempotencyStateTransition(String from, String to, String provider, GenerationStage stage) {
  }

  default void recordUnknownOutcomeTerminal(String provider, GenerationStage stage) {
  }

  default void recordProviderRetry(String provider, int attempt, String reason) {
  }

  default void recordSecretRefresh(String provider) {
  }

  static GenerationMetrics noop() {
    return new GenerationMetrics() {
      @Override
      public void recordStageLatency(GenerationJob job, GenerationStage stage, Duration duration, boolean success, String errorCode) {
      }

      @Override
      public void recordArtifactCost(GenerationJob job, GenerationStage stage, String provider, double usd) {
      }

      @Override
      public void recordBudgetUsage(GenerationJob job, double reservedUsd, double dailyCapUsd) {
      }

      @Override
      public void recordBudgetCommitted(GenerationJob job, double usedUsd) {
      }

      @Override
      public void recordAdmissionVerdict(String tenantId, String tier, String decision, String reason) {
      }

      @Override
      public void recordAdmissionRejected(String tenantId, String reason) {
      }

      @Override
      public void recordSafetyDecision(GenerationJob job, GenerationStage stage, String gate, boolean allowed,
          String reason, String classifier) {
      }

      @Override
      public void recordWatermarkVerification(GenerationJob job, boolean present) {
      }

      @Override
      public void recordStageRetry(GenerationJob job, GenerationStage stage, int nextAttempt, String errorCode) {
      }
    };
  }
}
