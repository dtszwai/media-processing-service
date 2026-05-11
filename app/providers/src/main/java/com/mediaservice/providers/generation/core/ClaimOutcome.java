package com.mediaservice.providers.generation.core;

/**
 * Result of attempting to claim an idempotency row for a side-effecting stage.
 * The five-state idempotency machine — {@code claimed | completed | failed |
 * unknown_outcome} plus the implicit pre-claim state — collapses to one of
 * these caller-visible outcomes.
 */
public sealed interface ClaimOutcome
    permits ClaimOutcome.Proceed,
            ClaimOutcome.ReuseExisting,
            ClaimOutcome.Reconcile,
            ClaimOutcome.ExitRedeliver,
            ClaimOutcome.TerminalFailure {

  /** Caller has the claim; proceed with the paid call. */
  record Proceed(String idempotencyKey) implements ClaimOutcome {
  }

  /** Prior attempt completed; reuse the stored result and skip the paid call. */
  record ReuseExisting(String resultRef) implements ClaimOutcome {
  }

  /**
   * Prior attempt crashed between "provider accepted" and "row completed". The
   * caller must attempt provider-side reconciliation before any new paid call.
   */
  record Reconcile(String idempotencyKey) implements ClaimOutcome {
  }

  /**
   * Another worker holds an in-flight claim (lease still valid) or is currently
   * reconciling an unknown_outcome row. The caller must throw a transient
   * exception so SQS redelivers after the visibility timeout.
   */
  record ExitRedeliver() implements ClaimOutcome {
  }

  /** Idempotency row terminated with failure; do not re-claim. */
  record TerminalFailure(String errorCode, String errorMessage) implements ClaimOutcome {
  }
}
