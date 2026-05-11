package com.mediaservice.providers.generation.llm;

import com.mediaservice.providers.generation.llm.LlmProvider;
import com.mediaservice.common.generation.provider.ProviderKind;

/**
 * Deterministic LLM stub used in dev/CI. Returns a stable string derived from the
 * supplied prompt so workflow tests stay reproducible.
 */
public class SimulatedLlmProvider implements LlmProvider {
  @Override
  public String generate(String prompt, String model) {
    String safePrompt = prompt != null ? prompt : "";
    String safeModel = model != null && !model.isBlank() ? model : "simulator-llm-v1";
    return "AI-generated audio overview. [" + safeModel + "] " + safePrompt;
  }

  @Override
  public ProviderKind kind() {
    return ProviderKind.SYNC;
  }
}
