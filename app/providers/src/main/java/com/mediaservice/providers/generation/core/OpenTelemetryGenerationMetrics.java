package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationStage;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.DoubleHistogram;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import java.time.Duration;

public class OpenTelemetryGenerationMetrics implements GenerationMetrics {
  private static final AttributeKey<String> SERVICE = AttributeKey.stringKey("service");
  private static final AttributeKey<String> REGION = AttributeKey.stringKey("region");
  private static final AttributeKey<String> OUTPUT_TYPE = AttributeKey.stringKey("output_type");
  private static final AttributeKey<String> STAGE = AttributeKey.stringKey("stage");
  private static final AttributeKey<String> PROVIDER = AttributeKey.stringKey("provider");
  private static final AttributeKey<String> TIER = AttributeKey.stringKey("tier");
  private static final AttributeKey<String> OUTCOME = AttributeKey.stringKey("outcome");
  private static final AttributeKey<String> ERROR_CODE = AttributeKey.stringKey("error_code");
  private static final AttributeKey<String> REASON = AttributeKey.stringKey("reason");
  private static final AttributeKey<String> GATE = AttributeKey.stringKey("gate");
  private static final AttributeKey<String> CLASSIFIER = AttributeKey.stringKey("classifier");
  private static final AttributeKey<String> FROM_STATE = AttributeKey.stringKey("from");
  private static final AttributeKey<String> TO_STATE = AttributeKey.stringKey("to");
  private static final AttributeKey<Long> ATTEMPT = AttributeKey.longKey("attempt");

  private final String serviceName;
  private final String region;
  private final DoubleHistogram stageLatencyMs;
  private final DoubleHistogram artifactCostUsd;
  private final DoubleHistogram budgetUsedPct;
  private final DoubleHistogram budgetReservedUsd;
  private final DoubleHistogram budgetUsedUsd;
  private final LongCounter admissionVerdicts;
  private final LongCounter rejectedAdmissions;
  private final LongCounter safetyDecisions;
  private final LongCounter watermarkVerifications;
  private final LongCounter stageRetries;
  private final LongCounter idempotencyStateTransitions;
  private final LongCounter unknownOutcomeTerminals;
  private final LongCounter providerRetries;
  private final LongCounter secretRefreshes;

  public OpenTelemetryGenerationMetrics(Meter meter, String serviceName, String region) {
    this.serviceName = blankToUnknown(serviceName);
    this.region = blankToUnknown(region);
    this.stageLatencyMs = meter.histogramBuilder("generation.stage.latency_ms")
        .setDescription("Generation stage latency in milliseconds")
        .setUnit("ms")
        .build();
    this.artifactCostUsd = meter.histogramBuilder("generation.artifact.cost_usd")
        .setDescription("Estimated or committed generation cost per artifact")
        .setUnit("USD")
        .build();
    this.budgetUsedPct = meter.histogramBuilder("generation.budget.used_pct")
        .setDescription("Reserved generation budget as a percent of the daily cap")
        .setUnit("percent")
        .build();
    this.budgetReservedUsd = meter.histogramBuilder("generation.budget.reserved_usd")
        .setDescription("Reserved generation budget in USD")
        .setUnit("USD")
        .build();
    this.budgetUsedUsd = meter.histogramBuilder("generation.budget.used_usd")
        .setDescription("Committed generation budget in USD")
        .setUnit("USD")
        .build();
    this.admissionVerdicts = meter.counterBuilder("generation.admission.verdict")
        .setDescription("Generation admission verdicts")
        .build();
    this.rejectedAdmissions = meter.counterBuilder("generation.admission.rejected")
        .setDescription("Generation submissions rejected by admission control")
        .build();
    this.safetyDecisions = meter.counterBuilder("generation.safety.decision")
        .setDescription("Generation safety decisions by gate")
        .build();
    this.watermarkVerifications = meter.counterBuilder("generation.watermark.verification")
        .setDescription("Generated image watermark verification decisions")
        .build();
    this.stageRetries = meter.counterBuilder("generation.stage.retry")
        .setDescription("Generation stage retry events")
        .build();
    this.idempotencyStateTransitions = meter.counterBuilder("generation.idempotency.state_transition")
        .setDescription("Idempotency row state transitions")
        .build();
    this.unknownOutcomeTerminals = meter.counterBuilder("generation.idempotency.unknown_outcome_terminal")
        .setDescription("Idempotency rows that terminated as unrecoverable unknown outcome")
        .build();
    this.providerRetries = meter.counterBuilder("generation.provider.retry")
        .setDescription("Provider HTTP call retries")
        .build();
    this.secretRefreshes = meter.counterBuilder("generation.provider.secret_refresh")
        .setDescription("Provider secret refresh events")
        .build();
  }

  @Override
  public void recordStageLatency(GenerationJob job, GenerationStage stage, Duration duration, boolean success, String errorCode) {
    stageLatencyMs.record(duration.toNanos() / 1_000_000.0,
        base(job, stage, "workflow", success ? "success" : "failure", errorCode));
  }

  @Override
  public void recordArtifactCost(GenerationJob job, GenerationStage stage, String provider, double usd) {
    artifactCostUsd.record(usd, base(job, stage, provider, "committed", null));
  }

  @Override
  public void recordBudgetUsage(GenerationJob job, double reservedUsd, double dailyCapUsd) {
    Attributes attrs = base(job, GenerationStage.ADMISSION, "budget", "reserved", null);
    budgetReservedUsd.record(reservedUsd, attrs);
    if (dailyCapUsd > 0) {
      budgetUsedPct.record((reservedUsd / dailyCapUsd) * 100.0, attrs);
    }
  }

  @Override
  public void recordBudgetCommitted(GenerationJob job, double usedUsd) {
    budgetUsedUsd.record(usedUsd, base(job, job != null ? job.getCurrentStage() : null, "budget", "committed", null));
  }

  @Override
  public void recordAdmissionVerdict(String tenantId, String tier, String decision, String reason) {
    admissionVerdicts.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(TIER, blankToUnknown(tier))
        .put(OUTCOME, blankToUnknown(decision))
        .put(REASON, blankToUnknown(reason))
        .build());
  }

  @Override
  public void recordAdmissionRejected(String tenantId, String reason) {
    rejectedAdmissions.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(TIER, "default")
        .put(REASON, blankToUnknown(reason))
        .build());
  }

  @Override
  public void recordSafetyDecision(GenerationJob job, GenerationStage stage, String gate, boolean allowed,
      String reason, String classifier) {
    safetyDecisions.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(OUTPUT_TYPE, job != null && job.getOutputType() != null ? job.getOutputType().name() : "unknown")
        .put(STAGE, stage != null ? stage.name() : "unknown")
        .put(TIER, tier(job))
        .put(GATE, blankToUnknown(gate))
        .put(OUTCOME, allowed ? "allowed" : "blocked")
        .put(REASON, blankToUnknown(reason))
        .put(CLASSIFIER, blankToUnknown(classifier))
        .build());
  }

  @Override
  public void recordWatermarkVerification(GenerationJob job, boolean present) {
    watermarkVerifications.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(OUTPUT_TYPE, job != null && job.getOutputType() != null ? job.getOutputType().name() : "unknown")
        .put(TIER, tier(job))
        .put(OUTCOME, present ? "present" : "missing")
        .build());
  }

  @Override
  public void recordStageRetry(GenerationJob job, GenerationStage stage, int nextAttempt, String errorCode) {
    stageRetries.add(1, base(job, stage, "workflow", "retry_attempt_" + nextAttempt, errorCode));
  }

  @Override
  public void recordIdempotencyStateTransition(String from, String to, String provider, GenerationStage stage) {
    idempotencyStateTransitions.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(STAGE, stage != null ? stage.name() : "unknown")
        .put(PROVIDER, blankToUnknown(provider))
        .put(FROM_STATE, blankToUnknown(from))
        .put(TO_STATE, blankToUnknown(to))
        .build());
  }

  @Override
  public void recordUnknownOutcomeTerminal(String provider, GenerationStage stage) {
    unknownOutcomeTerminals.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(STAGE, stage != null ? stage.name() : "unknown")
        .put(PROVIDER, blankToUnknown(provider))
        .build());
  }

  @Override
  public void recordProviderRetry(String provider, int attempt, String reason) {
    providerRetries.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(PROVIDER, blankToUnknown(provider))
        .put(ATTEMPT, (long) attempt)
        .put(REASON, blankToUnknown(reason))
        .build());
  }

  @Override
  public void recordSecretRefresh(String provider) {
    secretRefreshes.add(1, Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(PROVIDER, blankToUnknown(provider))
        .build());
  }

  private Attributes base(GenerationJob job, GenerationStage stage, String provider, String outcome, String errorCode) {
    return Attributes.builder()
        .put(SERVICE, serviceName)
        .put(REGION, region)
        .put(OUTPUT_TYPE, job != null && job.getOutputType() != null ? job.getOutputType().name() : "unknown")
        .put(STAGE, stage != null ? stage.name() : "unknown")
        .put(PROVIDER, blankToUnknown(provider))
        .put(TIER, tier(job))
        .put(OUTCOME, blankToUnknown(outcome))
        .put(ERROR_CODE, blankToUnknown(errorCode))
        .build();
  }

  private String tier(GenerationJob job) {
    return job != null && job.getTier() != null && !job.getTier().isBlank() ? job.getTier() : "free";
  }

  private static String blankToUnknown(String value) {
    return value == null || value.isBlank() ? "unknown" : value;
  }
}
