package com.mediaservice.shared.admin.dlq;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.aspectj.lang.ProceedingJoinPoint;
import org.aspectj.lang.annotation.Around;
import org.aspectj.lang.annotation.Aspect;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;

import java.util.Map;

/**
 * Aspect that checks DLQ configuration for controller methods annotated with {@link RequiresDlqConfigured}.
 *
 * <p>This aspect intercepts annotated methods and:
 * <ol>
 *   <li>Checks if DLQ is configured via DlqAdminService</li>
 *   <li>Returns 503 Service Unavailable with error body if not configured</li>
 *   <li>Proceeds with the method if configured</li>
 * </ol>
 */
@Slf4j
@Aspect
@Component
@Order(1)
@RequiredArgsConstructor
public class DlqConfigurationAspect {

    private final DlqAdminService dlqAdminService;

    @Around("@annotation(RequiresDlqConfigured)")
    public Object checkDlqConfiguration(ProceedingJoinPoint joinPoint) throws Throwable {
        if (!dlqAdminService.isConfigured()) {
            log.debug("DLQ not configured, returning 503 for method: {}", joinPoint.getSignature().getName());
            return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
                    .body(Map.of(
                            "configured", false,
                            "message", "DLQ URL not configured"
                    ));
        }

        return joinPoint.proceed();
    }
}
