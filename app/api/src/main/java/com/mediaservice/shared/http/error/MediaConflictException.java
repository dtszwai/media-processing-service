package com.mediaservice.shared.http.error;

public class MediaConflictException extends RuntimeException {
  public MediaConflictException(String message) {
    super(message);
  }
}
