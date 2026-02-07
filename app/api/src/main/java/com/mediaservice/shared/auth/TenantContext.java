package com.mediaservice.shared.auth;

public final class TenantContext {
  private static final String DEFAULT_TENANT = "default";
  private static final ThreadLocal<AuthPrincipal> currentPrincipal = new ThreadLocal<>();

  private TenantContext() {}

  public static void set(AuthPrincipal principal) {
    currentPrincipal.set(principal);
  }

  public static AuthPrincipal getPrincipal() {
    return currentPrincipal.get();
  }

  public static String getTenantId() {
    var principal = currentPrincipal.get();
    return principal != null ? principal.tenantId() : DEFAULT_TENANT;
  }

  public static String getUserId() {
    var principal = currentPrincipal.get();
    return principal != null ? principal.userId() : null;
  }

  public static boolean isAuthenticated() {
    return currentPrincipal.get() != null;
  }

  public static void clear() {
    currentPrincipal.remove();
  }
}
