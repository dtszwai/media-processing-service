package com.mediaservice.exception;

/**
 * Exception thrown when image validation fails.
 * This includes invalid file format, corrupted images, or dimension violations.
 */
public class InvalidImageException extends RuntimeException {
  public InvalidImageException(String message) {
    super(message);
  }

  public InvalidImageException(String message, Throwable cause) {
    super(message, cause);
  }
}
