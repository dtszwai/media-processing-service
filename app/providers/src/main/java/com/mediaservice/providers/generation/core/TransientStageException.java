package com.mediaservice.providers.generation.core;

/**
 * Marker exception thrown when a stage cannot complete because another worker
 * is still holding (or reconciling) the idempotency row. Surfaces to the
 * Lambda/SQS layer so the message is redelivered after the visibility timeout
 * rather than being marked as a permanent stage failure.
 */
public class TransientStageException extends RuntimeException {
  public TransientStageException(String message) {
    super(message);
  }
}
