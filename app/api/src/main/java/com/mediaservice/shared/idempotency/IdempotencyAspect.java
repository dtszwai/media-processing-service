package com.mediaservice.shared.idempotency;

import jakarta.servlet.http.HttpServletRequest;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.aspectj.lang.ProceedingJoinPoint;
import org.aspectj.lang.annotation.Around;
import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.reflect.MethodSignature;
import org.springframework.core.annotation.Order;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.web.context.request.RequestContextHolder;
import org.springframework.web.context.request.ServletRequestAttributes;

import java.lang.reflect.ParameterizedType;
import java.lang.reflect.Type;

/**
 * Aspect that handles idempotency for controller methods annotated with {@link Idempotent}.
 *
 * <p>This aspect intercepts annotated methods and:
 * <ol>
 *   <li>Extracts the Idempotency-Key header from the request</li>
 *   <li>If key present, checks for duplicate request</li>
 *   <li>Returns cached response if duplicate with completed response</li>
 *   <li>Returns 409 Conflict if duplicate still processing</li>
 *   <li>Executes method and caches response if new request</li>
 * </ol>
 */
@Slf4j
@Aspect
@Component
@Order(1)
@RequiredArgsConstructor
public class IdempotencyAspect {

    private static final String IDEMPOTENCY_KEY_HEADER = "Idempotency-Key";

    private final IdempotencyService idempotencyService;

    @Around("@annotation(idempotent)")
    public Object handleIdempotency(ProceedingJoinPoint joinPoint, Idempotent idempotent) throws Throwable {
        String idempotencyKey = extractIdempotencyKey();

        // No idempotency key - proceed normally
        if (idempotencyKey == null || idempotencyKey.isEmpty()) {
            return joinPoint.proceed();
        }

        String scope = idempotent.scope();
        Class<?> responseBodyType = extractResponseBodyType(joinPoint);

        // Check for duplicate
        var result = idempotencyService.checkAndSet(idempotencyKey, scope, responseBodyType);
        if (result.isDuplicate()) {
            if (result.cachedResponse() != null) {
                log.info("Returning cached response for idempotency key: {}, scope: {}", idempotencyKey, scope);
                return ResponseEntity.status(getSuccessStatus(joinPoint)).body(result.cachedResponse());
            }
            // Request still in progress - return 409 Conflict
            log.info("Duplicate request still processing for idempotency key: {}, scope: {}", idempotencyKey, scope);
            return ResponseEntity.status(HttpStatus.CONFLICT).build();
        }

        // New request - proceed and cache response
        Object response = joinPoint.proceed();

        if (response instanceof ResponseEntity<?> responseEntity && responseEntity.getBody() != null) {
            idempotencyService.storeResponse(idempotencyKey, scope, responseEntity.getBody());
        }

        return response;
    }

    private String extractIdempotencyKey() {
        ServletRequestAttributes attrs = (ServletRequestAttributes) RequestContextHolder.getRequestAttributes();
        if (attrs == null) {
            return null;
        }
        HttpServletRequest request = attrs.getRequest();
        return request.getHeader(IDEMPOTENCY_KEY_HEADER);
    }

    private Class<?> extractResponseBodyType(ProceedingJoinPoint joinPoint) {
        MethodSignature signature = (MethodSignature) joinPoint.getSignature();
        Type returnType = signature.getMethod().getGenericReturnType();

        if (returnType instanceof ParameterizedType parameterizedType) {
            Type[] typeArgs = parameterizedType.getActualTypeArguments();
            if (typeArgs.length > 0 && typeArgs[0] instanceof Class<?> bodyType) {
                return bodyType;
            }
        }

        return Object.class;
    }

    private HttpStatus getSuccessStatus(ProceedingJoinPoint joinPoint) {
        MethodSignature signature = (MethodSignature) joinPoint.getSignature();
        var method = signature.getMethod();

        // Check for @PostMapping and infer status
        if (method.isAnnotationPresent(org.springframework.web.bind.annotation.PostMapping.class)) {
            // Check endpoint path to determine appropriate status
            var postMapping = method.getAnnotation(org.springframework.web.bind.annotation.PostMapping.class);
            String[] paths = postMapping.value().length > 0 ? postMapping.value() : postMapping.path();
            for (String path : paths) {
                if (path.contains("/init")) {
                    return HttpStatus.CREATED;
                }
            }
            return HttpStatus.ACCEPTED;
        }

        return HttpStatus.OK;
    }
}
