package com.mediaservice.shared.http.filter;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.slf4j.MDC;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;
import java.util.UUID;

/**
 * Assigns unique request and correlation IDs to each HTTP request for
 * distributed tracing.
 *
 * <p>
 * Request ID: Unique identifier for this specific request. If
 * {@code X-Request-ID}
 * header is provided by the client, it's used; otherwise a UUID is generated.
 *
 * <p>
 * Correlation ID: Used to trace requests across multiple services. If
 * {@code X-Correlation-ID} header is provided, it's used; otherwise defaults to
 * the request ID.
 *
 * <p>
 * Both IDs are:
 * <ul>
 * <li>Added to SLF4J MDC for log correlation (keys: requestId,
 * correlationId)</li>
 * <li>Returned in response headers for client debugging</li>
 * <li>Included in error responses via
 * {@link com.mediaservice.dto.ErrorResponse}</li>
 * </ul>
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 2)
public class RequestIdFilter extends OncePerRequestFilter {
  public static final String REQUEST_ID_HEADER = "X-Request-ID";
  public static final String CORRELATION_ID_HEADER = "X-Correlation-ID";
  public static final String MDC_REQUEST_ID = "requestId";
  public static final String MDC_CORRELATION_ID = "correlationId";

  @Override
  protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response,
      FilterChain filterChain) throws ServletException, IOException {
    String requestId = request.getHeader(REQUEST_ID_HEADER);
    if (requestId == null || requestId.isBlank()) {
      requestId = UUID.randomUUID().toString();
    }

    String correlationId = request.getHeader(CORRELATION_ID_HEADER);
    if (correlationId == null || correlationId.isBlank()) {
      correlationId = requestId;
    }

    MDC.put(MDC_REQUEST_ID, requestId);
    MDC.put(MDC_CORRELATION_ID, correlationId);

    response.setHeader(REQUEST_ID_HEADER, requestId);
    response.setHeader(CORRELATION_ID_HEADER, correlationId);

    try {
      filterChain.doFilter(request, response);
    } finally {
      MDC.remove(MDC_REQUEST_ID);
      MDC.remove(MDC_CORRELATION_ID);
    }
  }
}
