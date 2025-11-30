package com.mediaservice.filter;

import com.github.benmanes.caffeine.cache.Cache;
import com.mediaservice.config.RateLimitingConfig;
import io.github.bucket4j.Bucket;
import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;

import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;
import java.util.concurrent.TimeUnit;

/**
 * Rate limiting filter using the token bucket algorithm (Bucket4j).
 *
 * <p>
 * Applies per-IP rate limits with different thresholds for general API
 * requests vs upload endpoints. Uses Caffeine cache for efficient bucket
 * storage.
 *
 * <p>
 * Rate limit headers returned:
 * <ul>
 * <li>{@code X-Rate-Limit-Remaining} - tokens remaining in current window</li>
 * <li>{@code X-Rate-Limit-Retry-After-Seconds} - seconds until retry (on
 * 429)</li>
 * </ul>
 *
 * <p>
 * Excluded paths: /actuator/**, /swagger/**, /v3/api-docs/**, /v1/media/health
 *
 * @see RateLimitingConfig for rate limit configuration
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE)
@RequiredArgsConstructor
@Slf4j
public class RateLimitingFilter extends OncePerRequestFilter {
  private final RateLimitingConfig rateLimitingConfig;
  private final Cache<String, Bucket> apiRateLimitCache;
  private final Cache<String, Bucket> uploadRateLimitCache;

  private static final String RATE_LIMIT_REMAINING_HEADER = "X-Rate-Limit-Remaining";
  private static final String RATE_LIMIT_RETRY_AFTER_HEADER = "X-Rate-Limit-Retry-After-Seconds";

  @Override
  protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response,
      FilterChain filterChain) throws ServletException, IOException {
    if (!rateLimitingConfig.isEnabled()) {
      filterChain.doFilter(request, response);
      return;
    }
    var clientIp = getClientIp(request);
    var path = request.getRequestURI();
    var isUploadEndpoint = isUploadEndpoint(path, request.getMethod());
    var bucket = resolveBucket(clientIp, isUploadEndpoint);
    var probe = bucket.tryConsumeAndReturnRemaining(1);

    if (probe.isConsumed()) {
      response.addHeader(RATE_LIMIT_REMAINING_HEADER, String.valueOf(probe.getRemainingTokens()));
      filterChain.doFilter(request, response);
    } else {
      long waitTimeSeconds = TimeUnit.NANOSECONDS.toSeconds(probe.getNanosToWaitForRefill());
      response.addHeader(RATE_LIMIT_RETRY_AFTER_HEADER, String.valueOf(waitTimeSeconds));
      response.setStatus(HttpStatus.TOO_MANY_REQUESTS.value());
      response.setContentType(MediaType.APPLICATION_JSON_VALUE);
      response.getWriter().write(
          "{\"message\":\"Rate limit exceeded. Please retry after " + waitTimeSeconds + " seconds.\",\"status\":429}");
      log.warn("Rate limit exceeded for client IP: {} on path: {}", clientIp, path);
    }
  }

  private Bucket resolveBucket(String clientIp, boolean isUploadEndpoint) {
    if (isUploadEndpoint) {
      return uploadRateLimitCache.get(clientIp, k -> rateLimitingConfig.createUploadBucket());
    }
    return apiRateLimitCache.get(clientIp, k -> rateLimitingConfig.createApiBucket());
  }

  private boolean isUploadEndpoint(String path, String method) {
    return "POST".equalsIgnoreCase(method) &&
        (path.contains("/upload") || path.endsWith("/v1/media"));
  }

  private String getClientIp(HttpServletRequest request) {
    String xForwardedFor = request.getHeader("X-Forwarded-For");
    if (xForwardedFor != null && !xForwardedFor.isEmpty()) {
      return xForwardedFor.split(",")[0].trim();
    }
    String xRealIp = request.getHeader("X-Real-IP");
    if (xRealIp != null && !xRealIp.isEmpty()) {
      return xRealIp;
    }
    return request.getRemoteAddr();
  }

  @Override
  protected boolean shouldNotFilter(HttpServletRequest request) {
    String path = request.getRequestURI();
    return path.startsWith("/actuator") ||
        path.startsWith("/swagger") ||
        path.startsWith("/v3/api-docs") ||
        path.equals("/v1/media/health");
  }
}
