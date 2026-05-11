package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationOutputType;

public record GenerationSubmission(
    String tenantId,
    String userId,
    String tier,
    GenerationOutputType outputType,
    String prompt,
    String model,
    String resolution,
    Long seed,
    String webhookUrl,
    String audioOverviewProvider
) {
  public GenerationSubmission(String tenantId, String userId, GenerationOutputType outputType, String prompt,
      String model, String resolution, Long seed, String webhookUrl) {
    this(tenantId, userId, null, outputType, prompt, model, resolution, seed, webhookUrl, null);
  }
}
