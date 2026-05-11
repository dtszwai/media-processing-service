package com.mediaservice.providers.generation.llm;

import com.mediaservice.providers.generation.core.GenerationProvider;

public interface LlmProvider extends GenerationProvider {
  String generate(String prompt, String model);
}
