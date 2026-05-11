package com.mediaservice.providers.generation.audio;

import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.common.generation.provider.JobSpec;
import com.mediaservice.providers.generation.core.GenerationProvider;

public interface AudioOverviewProvider extends GenerationProvider {
  Artifact generateOverview(JobSpec spec);

  /**
   * Cheap pre-flight readiness check. Lets the workflow fail-fast at submit time before
   * reserving budget or scheduling stages when an out-of-band credential (e.g. a captured
   * browser session) has expired. Providers with stable API-key auth return {@link AuthHealth#OK}.
   */
  default AuthHealth health() {
    return AuthHealth.OK;
  }

  enum AuthHealth {
    OK,
    AUTH_MISSING,
    AUTH_EXPIRED
  }
}
