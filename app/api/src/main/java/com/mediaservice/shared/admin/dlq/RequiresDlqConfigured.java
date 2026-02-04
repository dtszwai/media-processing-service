package com.mediaservice.shared.admin.dlq;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Marks a controller method as requiring DLQ configuration.
 *
 * <p>When present, the aspect will:
 * <ol>
 *   <li>Check if DLQ is configured via DlqAdminService.isConfigured()</li>
 *   <li>Return 503 Service Unavailable if not configured</li>
 *   <li>Proceed with the method if configured</li>
 * </ol>
 *
 * <p>Usage:
 * <pre>{@code
 * @RequiresDlqConfigured
 * @GetMapping("/status")
 * public ResponseEntity<Map<String, Object>> getStatus() { ... }
 * }</pre>
 */
@Target(ElementType.METHOD)
@Retention(RetentionPolicy.RUNTIME)
public @interface RequiresDlqConfigured {
}
