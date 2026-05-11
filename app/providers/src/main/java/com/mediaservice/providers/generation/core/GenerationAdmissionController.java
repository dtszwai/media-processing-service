package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationJob;

public interface GenerationAdmissionController {
  AdmissionVerdict evaluate(GenerationSubmission submission);

  default void recordAccepted(GenerationJob job) {
  }

  default void release(GenerationJob job) {
  }

  default void rollback(GenerationJob job) {
    release(job);
  }

  static GenerationAdmissionController allowAll() {
    return submission -> AdmissionVerdict.allow();
  }
}
