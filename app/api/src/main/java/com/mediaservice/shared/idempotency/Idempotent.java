package com.mediaservice.shared.idempotency;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Marks a controller method as idempotent.
 *
 * <p>When present, the aspect will:
 * <ol>
 *   <li>Extract the idempotency key from the Idempotency-Key header</li>
 *   <li>Check for duplicate requests and return cached response if found</li>
 *   <li>Execute the method if new request</li>
 *   <li>Cache the response for future duplicate requests</li>
 * </ol>
 *
 * <p>Usage:
 * <pre>{@code
 * @Idempotent(scope = "upload")
 * @PostMapping("/upload")
 * public ResponseEntity<MediaResponse> uploadMedia(...) { ... }
 * }</pre>
 */
@Target(ElementType.METHOD)
@Retention(RetentionPolicy.RUNTIME)
public @interface Idempotent {
    /**
     * The scope/operation name for namespacing idempotency keys in Redis.
     */
    String scope();
}
