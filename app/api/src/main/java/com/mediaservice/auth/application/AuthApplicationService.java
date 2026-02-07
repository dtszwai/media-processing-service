package com.mediaservice.auth.application;

import com.mediaservice.auth.api.dto.AuthResponse;
import com.mediaservice.auth.api.dto.LoginRequest;
import com.mediaservice.auth.api.dto.RegisterRequest;
import com.mediaservice.auth.domain.model.Role;
import com.mediaservice.auth.domain.model.Tenant;
import com.mediaservice.auth.domain.model.User;
import com.mediaservice.auth.infrastructure.persistence.TenantDynamoDbRepository;
import com.mediaservice.auth.infrastructure.persistence.UserDynamoDbRepository;
import com.mediaservice.shared.auth.AuthProperties;
import com.mediaservice.shared.auth.JwtService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.security.crypto.password.PasswordEncoder;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

@Slf4j
@Service
@RequiredArgsConstructor
public class AuthApplicationService {
  private final TenantDynamoDbRepository tenantRepository;
  private final UserDynamoDbRepository userRepository;
  private final JwtService jwtService;
  private final PasswordEncoder passwordEncoder;
  private final AuthProperties authProperties;

  public AuthResponse register(RegisterRequest request) {
    // Check if email already exists
    if (userRepository.findByEmail(request.getEmail()).isPresent()) {
      throw new IllegalArgumentException("Email already registered");
    }

    String tenantId = UUID.randomUUID().toString();
    String userId = UUID.randomUUID().toString();
    var now = Instant.now();

    // Create tenant
    tenantRepository.createTenant(Tenant.builder()
        .tenantId(tenantId)
        .name(request.getTenantName())
        .plan("free")
        .createdAt(now)
        .build());

    // Create admin user
    List<String> roles = List.of(Role.ADMIN.name());
    userRepository.createUser(User.builder()
        .userId(userId)
        .tenantId(tenantId)
        .email(request.getEmail())
        .passwordHash(passwordEncoder.encode(request.getPassword()))
        .roles(roles)
        .createdAt(now)
        .build());

    String token = jwtService.createToken(userId, tenantId, request.getEmail(), roles);
    String refreshToken = jwtService.createRefreshToken(userId, tenantId);

    log.info("Registered tenant: {} with admin user: {}", tenantId, userId);

    return AuthResponse.builder()
        .token(token)
        .refreshToken(refreshToken)
        .tenantId(tenantId)
        .userId(userId)
        .expiresIn(authProperties.getJwt().getExpirationSeconds())
        .build();
  }

  public Optional<AuthResponse> login(LoginRequest request) {
    return userRepository.findByEmail(request.getEmail())
        .filter(user -> passwordEncoder.matches(request.getPassword(), user.getPasswordHash()))
        .map(user -> {
          String token = jwtService.createToken(user.getUserId(), user.getTenantId(), user.getEmail(), user.getRoles());
          String refreshToken = jwtService.createRefreshToken(user.getUserId(), user.getTenantId());

          log.info("User logged in: userId={}, tenantId={}", user.getUserId(), user.getTenantId());

          return AuthResponse.builder()
              .token(token)
              .refreshToken(refreshToken)
              .tenantId(user.getTenantId())
              .userId(user.getUserId())
              .expiresIn(authProperties.getJwt().getExpirationSeconds())
              .build();
        });
  }

  public Optional<AuthResponse> refresh(String refreshToken) {
    return jwtService.validateRefreshToken(refreshToken)
        .flatMap(claims -> {
          String userId = claims.getSubject();
          String tenantId = claims.get("tenantId", String.class);

          return userRepository.getUser(userId)
              .filter(user -> user.getTenantId().equals(tenantId))
              .map(user -> {
                String newToken = jwtService.createToken(userId, tenantId, user.getEmail(), user.getRoles());
                String newRefreshToken = jwtService.createRefreshToken(userId, tenantId);

                return AuthResponse.builder()
                    .token(newToken)
                    .refreshToken(newRefreshToken)
                    .tenantId(tenantId)
                    .userId(userId)
                    .expiresIn(authProperties.getJwt().getExpirationSeconds())
                    .build();
              });
        });
  }
}
