package com.mediaservice.providers.generation.llm;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.mediaservice.providers.generation.llm.LlmProvider;
import com.mediaservice.providers.generation.core.GenerationProviderException;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;
import com.mediaservice.providers.generation.core.NotConfiguredException;
import com.mediaservice.providers.generation.shared.OpenAIClient;

/**
 * OpenAI chat-completion-backed LLM provider. Routed through the shared
 * {@link OpenAIClient} so it inherits retry, jitter, and rotation-aware secret behaviour.
 */
public class OpenAILlmProvider implements LlmProvider {
  private final GenerationRuntimeConfig config;
  private final OpenAIClient client;

  public OpenAILlmProvider(GenerationRuntimeConfig config) {
    this(config, new OpenAIClient(config));
  }

  public OpenAILlmProvider(GenerationRuntimeConfig config, OpenAIClient client) {
    this.config = config;
    this.client = client;
  }

  @Override
  public String generate(String prompt, String model) {
    client.requireConfigured();
    String resolvedModel = resolveModel(model);
    ObjectNode body = OpenAIClient.mapper().createObjectNode();
    body.put("model", resolvedModel);
    ArrayNode messages = body.putArray("messages");
    ObjectNode message = messages.addObject();
    message.put("role", "user");
    message.put("content", prompt != null ? prompt : "");
    JsonNode response = client.postJson("/chat/completions", body, null);
    JsonNode content = response.path("choices").path(0).path("message").path("content");
    if (!content.isTextual()) {
      throw new GenerationProviderException(GenerationErrorCode.OPENAI_EMPTY_RESPONSE,
          "OpenAI chat completion did not include message content");
    }
    return content.asText();
  }

  private String resolveModel(String requested) {
    if (requested != null && !requested.isBlank() && !requested.startsWith("simulator")) {
      return requested;
    }
    String configured = config.llmModel();
    if (configured == null || configured.isBlank() || configured.startsWith("simulator")) {
      throw new NotConfiguredException("OpenAILlmProvider",
          "GENERATION_LLM_MODEL must be set when GENERATION_LLM_PROVIDER=openai");
    }
    return configured;
  }
}
