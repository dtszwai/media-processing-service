package com.mediaservice.shared.auth;

import java.util.List;

public record AuthPrincipal(
    String tenantId,
    String userId,
    String email,
    List<String> roles,
    AuthMethod authMethod
) {
  public enum AuthMethod {
    JWT,
    API_KEY
  }

  public boolean hasRole(String role) {
    return roles != null && roles.contains(role);
  }

  public boolean isAdmin() {
    return hasRole("ADMIN");
  }
}
