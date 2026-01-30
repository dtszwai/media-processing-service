package com.mediaservice.shared.idempotency;

/**
 * Result of idempotency check.
 *
 * @param isDuplicate true if request is a duplicate
 * @param cachedResponse the cached response if duplicate, null otherwise
 */
public record IdempotencyResult<T>(
    boolean isDuplicate,
    T cachedResponse
) {
    public static <T> IdempotencyResult<T> newRequest() {
        return new IdempotencyResult<>(false, null);
    }

    public static <T> IdempotencyResult<T> duplicate(T response) {
        return new IdempotencyResult<>(true, response);
    }
}
