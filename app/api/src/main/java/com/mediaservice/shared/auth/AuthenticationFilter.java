package com.mediaservice.shared.auth;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.core.Ordered;
import org.springframework.core.annotation.Order;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.authority.SimpleGrantedAuthority;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;
import java.util.Optional;

@Slf4j
@Component
@Order(Ordered.HIGHEST_PRECEDENCE + 3)
@RequiredArgsConstructor
public class AuthenticationFilter extends OncePerRequestFilter {
  private static final String BEARER_PREFIX = "Bearer ";

  private final JwtService jwtService;
  private final ApiKeyService apiKeyService;
  private final AuthProperties authProperties;

  @Override
  protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response,
      FilterChain filterChain) throws ServletException, IOException {
    try {
      Optional<AuthPrincipal> principal = extractPrincipal(request);
      principal.ifPresent(p -> {
        TenantContext.set(p);

        var authorities = p.roles() != null
            ? p.roles().stream().map(role -> new SimpleGrantedAuthority("ROLE_" + role)).toList()
            : java.util.List.<SimpleGrantedAuthority>of();

        var authentication = new UsernamePasswordAuthenticationToken(p, null, authorities);
        SecurityContextHolder.getContext().setAuthentication(authentication);

        log.debug("Authenticated: tenantId={}, userId={}, method={}", p.tenantId(), p.userId(), p.authMethod());
      });

      filterChain.doFilter(request, response);
    } finally {
      TenantContext.clear();
      SecurityContextHolder.clearContext();
    }
  }

  private Optional<AuthPrincipal> extractPrincipal(HttpServletRequest request) {
    // Try JWT Bearer token first
    String authHeader = request.getHeader("Authorization");
    if (authHeader != null && authHeader.startsWith(BEARER_PREFIX)) {
      String token = authHeader.substring(BEARER_PREFIX.length());
      return jwtService.validateToken(token);
    }

    // Try API key
    String apiKeyHeader = authProperties.getApiKey().getHeader();
    String apiKey = request.getHeader(apiKeyHeader);
    if (apiKey != null && !apiKey.isBlank()) {
      return apiKeyService.validateApiKey(apiKey);
    }

    return Optional.empty();
  }
}
