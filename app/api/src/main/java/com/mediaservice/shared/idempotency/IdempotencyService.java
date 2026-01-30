package com.mediaservice.shared.idempotency;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.stereotype.Service;

import java.time.Duration;

/**
 * Service for API-level idempotency using Redis-backed request deduplication.
 * Clients send an Idempotency-Key header; duplicate requests return cached response.
 */
@Slf4j
@Service
@RequiredArgsConstructor
public class IdempotencyService {

    private static final String KEY_PREFIX = "idempotency:";
    private static final Duration DEFAULT_TTL = Duration.ofHours(24);
    private static final String PROCESSING_MARKER = "PROCESSING";

    private final StringRedisTemplate redisTemplate;
    private final ObjectMapper objectMapper;

    /**
     * Check if request is duplicate and atomically set lock if new.
     *
     * @param idempotencyKey client-provided key
     * @param operation operation name for namespacing
     * @param responseType the class type to deserialize cached response
     * @return result indicating if duplicate and cached response if so
     */
    public <T> IdempotencyResult<T> checkAndSet(String idempotencyKey, String operation, Class<T> responseType) {
        String redisKey = KEY_PREFIX + operation + ":" + idempotencyKey;

        // Atomic set-if-absent
        Boolean isNew = redisTemplate.opsForValue().setIfAbsent(
            redisKey,
            PROCESSING_MARKER,
            DEFAULT_TTL
        );

        if (Boolean.TRUE.equals(isNew)) {
            log.debug("New idempotency key: {}", redisKey);
            return IdempotencyResult.newRequest();
        }

        // Key exists - check if we have a cached response
        String cached = redisTemplate.opsForValue().get(redisKey);
        if (cached == null || PROCESSING_MARKER.equals(cached)) {
            log.info("Duplicate request still processing: {}", redisKey);
            return IdempotencyResult.duplicate(null); // In-progress
        }

        try {
            T response = objectMapper.readValue(cached, responseType);
            log.info("Returning cached response for: {}", redisKey);
            return IdempotencyResult.duplicate(response);
        } catch (JsonProcessingException e) {
            log.error("Failed to deserialize cached response: {}", e.getMessage());
            return IdempotencyResult.duplicate(null);
        }
    }

    /**
     * Store the response for a completed request.
     *
     * @param idempotencyKey client-provided key
     * @param operation operation name for namespacing
     * @param response the response to cache
     */
    public <T> void storeResponse(String idempotencyKey, String operation, T response) {
        String redisKey = KEY_PREFIX + operation + ":" + idempotencyKey;
        try {
            String json = objectMapper.writeValueAsString(response);
            redisTemplate.opsForValue().set(redisKey, json, DEFAULT_TTL);
            log.debug("Stored response for: {}", redisKey);
        } catch (JsonProcessingException e) {
            log.error("Failed to serialize response: {}", e.getMessage());
        }
    }
}
