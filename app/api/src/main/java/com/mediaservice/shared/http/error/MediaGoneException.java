package com.mediaservice.shared.http.error;

import java.time.Instant;
import lombok.Getter;

/**
 * Exception thrown when accessing a soft-deleted media resource.
 * Results in HTTP 410 Gone response.
 */
@Getter
public class MediaGoneException extends RuntimeException {
  private final Instant deletedAt;

  public MediaGoneException(String message) {
    super(message);
    this.deletedAt = null;
  }

  public MediaGoneException(String message, Instant deletedAt) {
    super(message);
    this.deletedAt = deletedAt;
  }
}
