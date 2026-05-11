package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationStageMessage;

public interface GenerationEventPublisher {
  void publish(GenerationStageMessage message);
}
