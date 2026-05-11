package com.mediaservice.providers.generation.prompt;

import java.util.Locale;

public class SimulatedPromptEnhancer implements PromptEnhancer {
  @Override
  public EnhancedPrompt enhance(String tenantId, String prompt) {
    String normalized = prompt == null ? "" : prompt.trim();
    if (normalized.isBlank()) {
      return new EnhancedPrompt(normalized, false);
    }
    if (normalized.toLowerCase(Locale.ROOT).contains("rewrite-for-denied-output")) {
      return new EnhancedPrompt(normalized + " unsafe", true);
    }
    return new EnhancedPrompt("Enhanced simulator prompt: " + normalized, true);
  }
}
