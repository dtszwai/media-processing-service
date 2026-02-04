package com.mediaservice.media.application;

/**
 * Result type for preview operations.
 * Represents the different states a preview request can result in.
 */
public sealed interface PreviewResult {

    /**
     * Preview is ready with URL.
     */
    record Ready(String url) implements PreviewResult {}

    /**
     * Media is still being processed.
     */
    record Processing(String mediaId) implements PreviewResult {}

    /**
     * Media not found.
     */
    record NotFound() implements PreviewResult {}
}
