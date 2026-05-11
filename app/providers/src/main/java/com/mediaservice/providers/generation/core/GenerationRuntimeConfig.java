package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.provider.ProviderKind;
import java.time.Duration;
import java.util.Map;

public record GenerationRuntimeConfig(
    String provider,
    String moderationProvider,
    String audioOverviewProvider,
    String llmProvider,
    String region,
    String model,
    String llmModel,
    ProviderKind simulatorKind,
    long simulatorMeanDurationMs,
    long simulatorColdStartMs,
    double simulatorFailureRate,
    boolean simulatorChaosBusinessHoursEnabled,
    double simulatorChaosFailureRate,
    int simulatorChaosStartHourUtc,
    int simulatorChaosEndHourUtc,
    double dailyBudgetUsd,
    int budgetAlertPct,
    Duration providerTimeout,
    String openAiApiKey,
    String openAiApiKeySecretArn,
    boolean promptEnhancementEnabled,
    int maxStageAttempts,
    int maxProviderAttempts,
    long secretCacheTtlMillis,
    int maxPollAttempts
) {
  public static GenerationRuntimeConfig simulatorDefaults() {
    return new GenerationRuntimeConfig(
        "simulated",
        "simulated",
        "simulated",
        "simulated",
        "us-west-2",
        "simulator-v1",
        "simulator-llm-v1",
        ProviderKind.SYNC,
        10,
        0,
        0.0,
        false,
        0.05,
        16,
        1,
        50.0,
        80,
        Duration.ofSeconds(30),
        null,
        null,
        true,
        3,
        3,
        300_000L,
        200);
  }

  public static GenerationRuntimeConfig fromEnvironment(Map<String, String> env) {
    GenerationRuntimeConfig defaults = simulatorDefaults();
    return new GenerationRuntimeConfig(
        env.getOrDefault("GENERATION_PROVIDER", defaults.provider()),
        env.getOrDefault("GENERATION_MODERATION_PROVIDER", env.getOrDefault("GENERATION_PROVIDER", defaults.moderationProvider())),
        env.getOrDefault("GENERATION_AUDIO_OVERVIEW_PROVIDER", defaults.audioOverviewProvider()),
        env.getOrDefault("GENERATION_LLM_PROVIDER", env.getOrDefault("GENERATION_PROVIDER", defaults.llmProvider())),
        env.getOrDefault("GENERATION_REGION", env.getOrDefault("AWS_REGION", defaults.region())),
        env.getOrDefault("GENERATION_MODEL", defaults.model()),
        env.getOrDefault("GENERATION_LLM_MODEL", defaults.llmModel()),
        parseKind(env.get("GENERATION_SIMULATOR_KIND"), defaults.simulatorKind()),
        parseLong(env.get("GENERATION_SIMULATOR_MEAN_DURATION_MS"), defaults.simulatorMeanDurationMs()),
        parseLong(env.get("GENERATION_SIMULATOR_COLD_START_MS"), defaults.simulatorColdStartMs()),
        parseDouble(env.get("GENERATION_SIMULATOR_FAILURE_RATE"), defaults.simulatorFailureRate()),
        parseBoolean(env.get("GENERATION_SIMULATOR_CHAOS_BUSINESS_HOURS_ENABLED"), defaults.simulatorChaosBusinessHoursEnabled()),
        parseDouble(env.get("GENERATION_SIMULATOR_CHAOS_FAILURE_RATE"), defaults.simulatorChaosFailureRate()),
        (int) parseLong(env.get("GENERATION_SIMULATOR_CHAOS_START_HOUR_UTC"), defaults.simulatorChaosStartHourUtc()),
        (int) parseLong(env.get("GENERATION_SIMULATOR_CHAOS_END_HOUR_UTC"), defaults.simulatorChaosEndHourUtc()),
        parseDouble(env.get("GENERATION_BUDGET_DAILY_USD"), defaults.dailyBudgetUsd()),
        (int) parseLong(env.get("GENERATION_BUDGET_ALERT_PCT"), defaults.budgetAlertPct()),
        Duration.ofMillis(parseLong(env.get("GENERATION_PROVIDER_TIMEOUT_MS"), defaults.providerTimeout().toMillis())),
        blankToNull(env.get("GENERATION_OPENAI_API_KEY")),
        blankToNull(env.get("GENERATION_OPENAI_API_KEY_SECRET_ARN")),
        parseBoolean(env.get("GENERATION_PROMPT_ENHANCEMENT_ENABLED"), defaults.promptEnhancementEnabled()),
        (int) parseLong(env.get("GENERATION_STAGE_MAX_ATTEMPTS"), defaults.maxStageAttempts()),
        (int) parseLong(env.get("GENERATION_PROVIDER_MAX_ATTEMPTS"), defaults.maxProviderAttempts()),
        parseLong(env.get("GENERATION_SECRET_CACHE_TTL_MS"), defaults.secretCacheTtlMillis()),
        (int) parseLong(env.get("GENERATION_POLL_MAX_ATTEMPTS"), defaults.maxPollAttempts()));
  }

  private static ProviderKind parseKind(String value, ProviderKind fallback) {
    if (value == null || value.isBlank()) {
      return fallback;
    }
    try {
      return ProviderKind.valueOf(value.trim().toUpperCase());
    } catch (IllegalArgumentException e) {
      return fallback;
    }
  }

  private static long parseLong(String value, long fallback) {
    if (value == null || value.isBlank()) {
      return fallback;
    }
    try {
      return Long.parseLong(value);
    } catch (NumberFormatException e) {
      return fallback;
    }
  }

  private static double parseDouble(String value, double fallback) {
    if (value == null || value.isBlank()) {
      return fallback;
    }
    try {
      return Double.parseDouble(value);
    } catch (NumberFormatException e) {
      return fallback;
    }
  }

  private static boolean parseBoolean(String value, boolean fallback) {
    if (value == null || value.isBlank()) {
      return fallback;
    }
    return Boolean.parseBoolean(value);
  }

  private static String blankToNull(String value) {
    return value == null || value.isBlank() ? null : value;
  }
}
