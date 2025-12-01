package com.mediaservice.shared.http.filter;

import com.mediaservice.shared.cache.config.RateLimitingConfig;
import com.mediaservice.shared.ratelimit.RedisRateLimitService;
import com.mediaservice.shared.ratelimit.RedisRateLimitService.RateLimitResult;
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

/**
 * Distributed rate limiting filter using Redis.
 *
 * <p>
 * Applies per-IP rate limits with different thresholds for general API
 * requests vs upload endpoints. Uses Redis for distributed rate limiting
 * across multiple API instances.
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
  private static final String BUCKET_TYPE_API = "api";
  private static final String BUCKET_TYPE_UPLOAD = "upload";

  private static final String RATE_LIMIT_REMAINING_HEADER = "X-Rate-Limit-Remaining";
  private static final String RATE_LIMIT_RETRY_AFTER_HEADER = "X-Rate-Limit-Retry-After-Seconds";

  private final RateLimitingConfig rateLimitingConfig;
  private final RedisRateLimitService rateLimitService;

  @Override
  protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response,
      FilterChain filterChain) throws ServletException, IOException {
    if (!rateLimitingConfig.isEnabled()) {
      filterChain.doFilter(request, response);
      return;
    }

    String clientIp = getClientIp(request);
    String path = request.getRequestURI();
    boolean isUploadEndpoint = isUploadEndpoint(path, request.getMethod());

    RateLimitResult result = checkRateLimit(clientIp, isUploadEndpoint);

    if (result.allowed()) {
      response.addHeader(RATE_LIMIT_REMAINING_HEADER, String.valueOf(result.remaining()));
      filterChain.doFilter(request, response);
    } else {
      response.addHeader(RATE_LIMIT_RETRY_AFTER_HEADER, String.valueOf(result.retryAfterSeconds()));
      response.setStatus(HttpStatus.TOO_MANY_REQUESTS.value());
      response.setContentType(MediaType.APPLICATION_JSON_VALUE);
      response.getWriter().write(
          "{\"message\":\"Rate limit exceeded. Please retry after " + result.retryAfterSeconds()
              + " seconds.\",\"status\":429}");
      log.warn("Rate limit exceeded for client IP: {} on path: {}", clientIp, path);
    }
  }

  private RateLimitResult checkRateLimit(String clientIp, boolean isUploadEndpoint) {
    if (isUploadEndpoint) {
      return rateLimitService.tryConsume(
          clientIp,
          BUCKET_TYPE_UPLOAD,
          rateLimitingConfig.getUpload().getRequestsPerMinute(),
          rateLimitingConfig.getWindow());
    }
    return rateLimitService.tryConsume(
        clientIp,
        BUCKET_TYPE_API,
        rateLimitingConfig.getApi().getRequestsPerMinute(),
        rateLimitingConfig.getWindow());
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
