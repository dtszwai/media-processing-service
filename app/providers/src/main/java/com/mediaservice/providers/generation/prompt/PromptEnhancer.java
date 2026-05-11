package com.mediaservice.providers.generation.prompt;

public interface PromptEnhancer {
  EnhancedPrompt enhance(String tenantId, String prompt);
}
