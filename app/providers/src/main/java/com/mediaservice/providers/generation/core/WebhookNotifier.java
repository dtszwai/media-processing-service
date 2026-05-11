package com.mediaservice.providers.generation.core;

import com.mediaservice.common.model.Media;

public interface WebhookNotifier {
  void notifyComplete(Media media);
}
