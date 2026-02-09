package com.mediaservice.media.application;

/**
 * Result type for document text extraction access.
 */
public sealed interface DocumentTextResult {

  /**
   * Text is ready with URL.
   */
  record Ready(String url) implements DocumentTextResult {}

  /**
   * Text is ready with inline JSON content.
   */
  record ReadyInline(byte[] content) implements DocumentTextResult {}

  /**
   * Text is still being processed.
   */
  record Processing(String mediaId) implements DocumentTextResult {}

  /**
   * Text not found or unavailable.
   */
  record NotFound() implements DocumentTextResult {}
}
