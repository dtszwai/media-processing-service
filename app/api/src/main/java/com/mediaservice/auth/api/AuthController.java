package com.mediaservice.auth.api;

import com.mediaservice.auth.api.dto.*;
import com.mediaservice.auth.application.ApiKeyManagementService;
import com.mediaservice.auth.application.AuthApplicationService;
import com.mediaservice.shared.auth.TenantContext;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;

@RestController
@RequestMapping("/v1/auth")
@RequiredArgsConstructor
@Tag(name = "Authentication", description = "User registration, login, and API key management")
public class AuthController {
  private final AuthApplicationService authService;
  private final ApiKeyManagementService apiKeyService;

  @PostMapping("/register")
  @Operation(summary = "Register a new tenant and admin user")
  public ResponseEntity<AuthResponse> register(@Valid @RequestBody RegisterRequest request) {
    var response = authService.register(request);
    return ResponseEntity.status(HttpStatus.CREATED).body(response);
  }

  @PostMapping("/login")
  @Operation(summary = "Login with email and password")
  public ResponseEntity<AuthResponse> login(@Valid @RequestBody LoginRequest request) {
    return authService.login(request)
        .map(ResponseEntity::ok)
        .orElse(ResponseEntity.status(HttpStatus.UNAUTHORIZED).build());
  }

  @PostMapping("/refresh")
  @Operation(summary = "Refresh access token")
  public ResponseEntity<AuthResponse> refresh(@Valid @RequestBody RefreshRequest request) {
    return authService.refresh(request.getRefreshToken())
        .map(ResponseEntity::ok)
        .orElse(ResponseEntity.status(HttpStatus.UNAUTHORIZED).build());
  }

  @GetMapping("/me")
  @Operation(summary = "Get current user info")
  public ResponseEntity<Object> me() {
    var principal = TenantContext.getPrincipal();
    if (principal == null) {
      return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
    }
    return ResponseEntity.ok(new UserInfo(principal.tenantId(), principal.userId(), principal.email(), principal.roles()));
  }

  @PostMapping("/api-keys")
  @Operation(summary = "Create a new API key")
  public ResponseEntity<ApiKeyResponse> createApiKey(@Valid @RequestBody CreateApiKeyRequest request) {
    var principal = TenantContext.getPrincipal();
    if (principal == null) {
      return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
    }
    var response = apiKeyService.createApiKey(principal.tenantId(), request.getName());
    return ResponseEntity.status(HttpStatus.CREATED).body(response);
  }

  @GetMapping("/api-keys")
  @Operation(summary = "List tenant's API keys")
  public ResponseEntity<List<ApiKeyResponse>> listApiKeys() {
    var principal = TenantContext.getPrincipal();
    if (principal == null) {
      return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
    }
    return ResponseEntity.ok(apiKeyService.listApiKeys(principal.tenantId()));
  }

  @DeleteMapping("/api-keys/{keyId}")
  @Operation(summary = "Revoke an API key")
  public ResponseEntity<Void> revokeApiKey(@PathVariable String keyId) {
    var principal = TenantContext.getPrincipal();
    if (principal == null) {
      return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
    }
    boolean revoked = apiKeyService.revokeApiKey(principal.tenantId(), keyId);
    return revoked ? ResponseEntity.noContent().build() : ResponseEntity.notFound().build();
  }

  record UserInfo(String tenantId, String userId, String email, java.util.List<String> roles) {}
}
