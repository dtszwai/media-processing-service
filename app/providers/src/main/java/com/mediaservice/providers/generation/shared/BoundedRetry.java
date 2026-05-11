package com.mediaservice.providers.generation.shared;

import java.util.concurrent.ThreadLocalRandom;

/** Bounded retry with exponential backoff and full jitter for fire-and-forget side-channel calls. */
public final class BoundedRetry {
  private BoundedRetry() {}

  public interface ThrowingRunnable {
    void run() throws Exception;
  }

  public static void run(int maxAttempts, long baseDelayMs, long maxDelayMs, ThrowingRunnable action) {
    int attempts = Math.max(1, maxAttempts);
    Throwable last = null;
    for (int attempt = 1; attempt <= attempts; attempt++) {
      try {
        action.run();
        return;
      } catch (Exception e) {
        last = e;
        if (attempt == attempts) break;
        sleep(jitter(baseDelayMs, maxDelayMs, attempt));
      }
    }
    if (last instanceof RuntimeException re) throw re;
    throw new RuntimeException("BoundedRetry exhausted", last);
  }

  static long jitter(long baseDelayMs, long maxDelayMs, int attempt) {
    long expo = baseDelayMs * (1L << Math.min(attempt - 1, 30));
    long capped = Math.min(expo, maxDelayMs);
    return ThreadLocalRandom.current().nextLong(0, Math.max(1, capped) + 1);
  }

  private static void sleep(long millis) {
    if (millis <= 0) return;
    try {
      Thread.sleep(millis);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
    }
  }
}
