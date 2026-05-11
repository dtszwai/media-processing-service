package com.mediaservice.providers.generation.image;

import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.common.generation.provider.JobSpec;
import com.mediaservice.common.generation.provider.ProviderJobId;
import com.mediaservice.common.generation.provider.ProviderState;
import com.mediaservice.providers.generation.core.GenerationProvider;
import java.util.Optional;

public interface ImageProvider extends GenerationProvider {
  Artifact generateSync(JobSpec spec);

  /**
   * Submit an async generation job. The default implementation throws
   * {@link UnsupportedOperationException}; only providers declaring
   * {@link com.mediaservice.common.generation.provider.ProviderKind#ASYNC}
   * must override this method.
   */
  default ProviderJobId submitAsync(JobSpec spec) {
    throw new UnsupportedOperationException("ASYNC not supported by " + getClass().getSimpleName());
  }

  /**
   * Poll the status of an in-flight async job. The default implementation throws; only ASYNC
   * providers must override this method.
   */
  default ProviderState poll(ProviderJobId providerJobId) {
    throw new UnsupportedOperationException("poll() not supported by " + getClass().getSimpleName());
  }

  /**
   * Fetch the artifact for a completed async job. The default implementation throws; only ASYNC
   * providers must override this method.
   */
  default Artifact fetch(ProviderJobId providerJobId) {
    throw new UnsupportedOperationException("fetch() not supported by " + getClass().getSimpleName());
  }

  /**
   * Best-effort reconciliation: query the provider for a prior submission by the
   * caller-supplied client request id (typically the workflow's idempotency key).
   * Providers that cannot address a previous job by an external id return empty,
   * forcing the caller to terminate the row as unrecoverable.
   */
  default Optional<Artifact> reconcile(String clientRequestId) {
    return Optional.empty();
  }
}
