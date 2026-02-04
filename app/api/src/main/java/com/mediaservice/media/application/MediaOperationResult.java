package com.mediaservice.media.application;

import com.mediaservice.common.model.Media;

/**
 * Result type for media operations (resize, retry, etc.).
 * Represents the different outcomes of an operation.
 */
public sealed interface MediaOperationResult {

    /**
     * Operation succeeded with the updated media.
     */
    record Success(Media media) implements MediaOperationResult {}

    /**
     * Media not found.
     */
    record NotFound(String mediaId) implements MediaOperationResult {}

    /**
     * Operation not allowed due to current media state.
     */
    record NotAllowed(String mediaId, String reason) implements MediaOperationResult {}

    /**
     * Media has been deleted.
     */
    record Deleted(String mediaId, java.time.Instant deletedAt) implements MediaOperationResult {}
}
