package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.provider.ProviderKind;

/**
 * Marker base for every provider that participates in the generation pipeline,
 * irrespective of output modality (image, audio, LLM, moderation, ...). Concrete
 * modality contracts (e.g. {@code ImageProvider}, {@code LlmProvider}) extend this
 * interface so the workflow and factory can reason about provider taxonomy uniformly.
 */
public interface GenerationProvider {
  default ProviderKind kind() {
    return ProviderKind.SYNC;
  }
}
