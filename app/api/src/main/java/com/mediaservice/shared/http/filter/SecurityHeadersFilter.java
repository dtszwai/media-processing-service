package com.mediaservice.shared.http.filter;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;

/**
 * Adds security headers to all HTTP responses for defense-in-depth protection.
 *
 * <p>
 * Headers applied:
 * <ul>
 * <li>{@code X-Content-Type-Options: nosniff} - prevents MIME type
 * sniffing</li>
 * <li>{@code X-Frame-Options: DENY} - prevents clickjacking</li>
 * <li>{@code X-XSS-Protection: 1; mode=block} - legacy XSS protection</li>
 * <li>{@code Referrer-Policy: strict-origin-when-cross-origin}</li>
 * <li>{@code Content-Security-Policy: default-src 'none'} - restrictive CSP for
 * API</li>
 * <li>{@code Permissions-Policy} - disables geolocation, microphone,
 * camera</li>
 * <li>{@code Strict-Transport-Security} - enforces HTTPS (HSTS)</li>
 * <li>{@code Cache-Control: no-store} - prevents caching (except Swagger
 * docs)</li>
 * </ul>
 */
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 1)
public class SecurityHeadersFilter extends OncePerRequestFilter {
  @Override
  protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response,
      FilterChain filterChain) throws ServletException, IOException {
    String uri = request.getRequestURI();
    boolean isSwaggerPath = uri.startsWith("/swagger") || uri.startsWith("/v3/api-docs");

    // Prevent MIME type sniffing
    response.setHeader("X-Content-Type-Options", "nosniff");
    // Prevent clickjacking (allow framing for Swagger UI)
    response.setHeader("X-Frame-Options", isSwaggerPath ? "SAMEORIGIN" : "DENY");
    // Enable XSS protection (legacy, but still useful for older browsers)
    response.setHeader("X-XSS-Protection", "1; mode=block");
    // Referrer policy
    response.setHeader("Referrer-Policy", "strict-origin-when-cross-origin");
    // Content Security Policy - permissive for Swagger, restrictive for API
    if (isSwaggerPath) {
      response.setHeader("Content-Security-Policy",
          "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;");
    } else {
      response.setHeader("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'");
    }
    // Permissions Policy
    response.setHeader("Permissions-Policy", "geolocation=(), microphone=(), camera=()");
    // Cache control for API responses (no caching by default, allow caching for
    // Swagger)
    if (!isSwaggerPath) {
      response.setHeader("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0");
      response.setHeader("Pragma", "no-cache");
    }
    // Strict Transport Security (HSTS) - browsers will only use HTTPS
    response.setHeader("Strict-Transport-Security", "max-age=31536000; includeSubDomains");
    filterChain.doFilter(request, response);
  }
}
