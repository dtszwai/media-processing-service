package com.mediaservice.providers.generation.image;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.common.generation.provider.JobSpec;
import com.mediaservice.common.generation.provider.ProviderKind;
import java.util.Base64;
import java.util.HashMap;
import java.util.Map;
import com.mediaservice.providers.generation.core.GenerationProviderException;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;
import com.mediaservice.providers.generation.core.NotConfiguredException;
import com.mediaservice.providers.generation.shared.OpenAIClient;

public class OpenAIImageProvider implements ImageProvider {
  private final GenerationRuntimeConfig config;
  private final OpenAIClient client;

  public OpenAIImageProvider(GenerationRuntimeConfig config) {
    this(config, new OpenAIClient(config));
  }

  public OpenAIImageProvider(GenerationRuntimeConfig config, OpenAIClient client) {
    this.config = config;
    this.client = client;
  }

  @Override
  public ProviderKind kind() {
    return ProviderKind.SYNC;
  }

  @Override
  public Artifact generateSync(JobSpec spec) {
    client.requireConfigured();
    if (spec == null) {
      throw new GenerationProviderException(GenerationErrorCode.INVALID_JOB_SPEC, "OpenAI image generation requires a job spec");
    }
    ObjectNode body = OpenAIClient.mapper().createObjectNode();
    String model = resolveImageModel(spec);
    body.put("model", model);
    body.put("prompt", spec.prompt());
    body.put("n", 1);
    if (spec.resolution() != null && !spec.resolution().isBlank()) {
      body.put("size", spec.resolution());
    }
    body.put("user", spec.tenantId());
    if (model.startsWith("dall-e")) {
      body.put("response_format", "b64_json");
    } else {
      body.put("output_format", "png");
      body.put("moderation", "auto");
    }

    JsonNode response = client.postJson("/images/generations", body, spec.jobId());
    JsonNode firstImage = response.path("data").path(0);
    if (firstImage.isMissingNode() || firstImage.isNull()) {
      throw new GenerationProviderException(GenerationErrorCode.OPENAI_EMPTY_RESPONSE, "OpenAI image response did not include image data");
    }

    byte[] bytes = decodeImage(firstImage);
    String format = firstImage.path("output_format").asText("png");
    Map<String, String> metadata = new HashMap<>();
    metadata.put("provider", "openai");
    metadata.put("model", model);
    metadata.put("watermark", "openai-generated");
    metadata.put("content_safety", "openai-generation-moderated");
    if (firstImage.path("revised_prompt").isTextual()) {
      metadata.put("revised_prompt", firstImage.path("revised_prompt").asText());
    }
    return new Artifact(bytes, contentType(format), "." + extension(format), metadata);
  }

  private byte[] decodeImage(JsonNode image) {
    if (image.path("b64_json").isTextual()) {
      return Base64.getDecoder().decode(image.path("b64_json").asText());
    }
    if (image.path("url").isTextual()) {
      return client.getBytes(image.path("url").asText());
    }
    throw new GenerationProviderException(GenerationErrorCode.OPENAI_EMPTY_RESPONSE, "OpenAI image response did not include b64_json or url");
  }

  private String resolveImageModel(JobSpec spec) {
    String model = spec.model() != null && !spec.model().isBlank() ? spec.model() : config.model();
    if (model == null || model.isBlank() || model.startsWith("simulator")) {
      throw new NotConfiguredException("OpenAIImageProvider",
          "GENERATION_MODEL must be set when GENERATION_PROVIDER=openai");
    }
    return model;
  }

  private String extension(String format) {
    return switch (format.toLowerCase()) {
      case "jpeg", "jpg" -> "jpg";
      case "webp" -> "webp";
      default -> "png";
    };
  }

  private String contentType(String format) {
    return switch (extension(format)) {
      case "jpg" -> "image/jpeg";
      case "webp" -> "image/webp";
      default -> "image/png";
    };
  }
}
