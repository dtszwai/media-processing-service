package com.mediaservice.providers.generation.moderation;

import com.mediaservice.common.generation.provider.ModerationResult;
import com.mediaservice.providers.generation.core.GenerationProvider;

public interface ModerationProvider extends GenerationProvider {
  ModerationResult moderate(String tenantId, String input, String stage);
}
