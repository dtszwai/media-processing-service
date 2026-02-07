package com.mediaservice.shared.auth;

import io.jsonwebtoken.Claims;
import io.jsonwebtoken.JwtException;
import io.jsonwebtoken.Jwts;
import io.jsonwebtoken.security.Keys;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

import javax.crypto.SecretKey;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Date;
import java.util.List;
import java.util.Optional;

@Slf4j
@Service
public class JwtService {
  private final SecretKey signingKey;
  private final AuthProperties authProperties;

  public JwtService(AuthProperties authProperties) {
    this.authProperties = authProperties;
    this.signingKey = Keys.hmacShaKeyFor(
        authProperties.getJwt().getSecret().getBytes(StandardCharsets.UTF_8));
  }

  public String createToken(String userId, String tenantId, String email, List<String> roles) {
    var now = Instant.now();
    var expiration = now.plusSeconds(authProperties.getJwt().getExpirationSeconds());

    return Jwts.builder()
        .subject(userId)
        .claim("tenantId", tenantId)
        .claim("email", email)
        .claim("roles", roles)
        .issuer(authProperties.getJwt().getIssuer())
        .issuedAt(Date.from(now))
        .expiration(Date.from(expiration))
        .signWith(signingKey)
        .compact();
  }

  public String createRefreshToken(String userId, String tenantId) {
    var now = Instant.now();
    var expiration = now.plusSeconds(authProperties.getJwt().getRefreshExpirationSeconds());

    return Jwts.builder()
        .subject(userId)
        .claim("tenantId", tenantId)
        .claim("type", "refresh")
        .issuer(authProperties.getJwt().getIssuer())
        .issuedAt(Date.from(now))
        .expiration(Date.from(expiration))
        .signWith(signingKey)
        .compact();
  }

  public Optional<AuthPrincipal> validateToken(String token) {
    try {
      Claims claims = Jwts.parser()
          .verifyWith(signingKey)
          .requireIssuer(authProperties.getJwt().getIssuer())
          .build()
          .parseSignedClaims(token)
          .getPayload();

      String userId = claims.getSubject();
      String tenantId = claims.get("tenantId", String.class);
      String email = claims.get("email", String.class);
      @SuppressWarnings("unchecked")
      List<String> roles = claims.get("roles", List.class);

      if (userId == null || tenantId == null) {
        log.warn("JWT missing required claims: userId={}, tenantId={}", userId, tenantId);
        return Optional.empty();
      }

      return Optional.of(new AuthPrincipal(tenantId, userId, email, roles, AuthPrincipal.AuthMethod.JWT));
    } catch (JwtException e) {
      log.debug("JWT validation failed: {}", e.getMessage());
      return Optional.empty();
    }
  }

  public Optional<Claims> validateRefreshToken(String token) {
    try {
      Claims claims = Jwts.parser()
          .verifyWith(signingKey)
          .requireIssuer(authProperties.getJwt().getIssuer())
          .build()
          .parseSignedClaims(token)
          .getPayload();

      String type = claims.get("type", String.class);
      if (!"refresh".equals(type)) {
        log.debug("Token is not a refresh token");
        return Optional.empty();
      }

      return Optional.of(claims);
    } catch (JwtException e) {
      log.debug("Refresh token validation failed: {}", e.getMessage());
      return Optional.empty();
    }
  }
}
