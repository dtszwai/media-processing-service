package com.mediaservice.providers.generation.prompt;

public class NoopPromptEnhancer implements PromptEnhancer {
  @Override
  public EnhancedPrompt enhance(String tenantId, String prompt) {
    return new EnhancedPrompt(prompt, false);
  }
}
