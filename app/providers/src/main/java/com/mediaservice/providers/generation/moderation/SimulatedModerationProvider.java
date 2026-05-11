package com.mediaservice.providers.generation.moderation;

import com.mediaservice.providers.generation.moderation.ModerationProvider;
import com.mediaservice.common.generation.provider.ModerationResult;
import java.util.List;
import java.util.Locale;

public class SimulatedModerationProvider implements ModerationProvider {
  private static final List<String> BLOCKED_TERMS = List.of("unsafe", "blocked", "violence", "self-harm", "illegal");

  @Override
  public ModerationResult moderate(String tenantId, String input, String stage) {
    String normalized = input == null ? "" : input.toLowerCase(Locale.ROOT);
    for (String term : BLOCKED_TERMS) {
      if (normalized.contains(term)) {
        return new ModerationResult(false, "simulated-moderation", "simulator-v1", 0.99,
            "blocked term: " + term);
      }
    }
    return new ModerationResult(true, "simulated-moderation", "simulator-v1", 0.01, "allowed");
  }
}
