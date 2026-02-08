package com.mediaservice.shared.auth;

import com.mediaservice.common.model.Media;
import lombok.RequiredArgsConstructor;
import org.springframework.security.access.AccessDeniedException;
import org.springframework.stereotype.Service;

@Service
@RequiredArgsConstructor
public class AuthorizationService {
  private final AuthProperties authProperties;

  /**
   * Verify the current user has access to the given media.
   * When enforcement is disabled, this is a no-op.
   *
   * @param media the media to check access for
   * @throws AccessDeniedException if the current user's tenant doesn't match
   */
  public void requireMediaAccess(Media media) {
    if (!authProperties.getEnforcement().isEnabled()) {
      return;
    }
    if (!TenantContext.isAuthenticated()) {
      throw new AccessDeniedException("Authentication required");
    }
    String currentTenantId = TenantContext.getTenantId();
    if (media.getTenantId() != null && !media.getTenantId().equals(currentTenantId)) {
      throw new AccessDeniedException("Access denied to this resource");
    }
  }

  /**
   * Verify the current user is authenticated.
   * When enforcement is disabled, this is a no-op.
   *
   * @throws AccessDeniedException if the current user is not authenticated
   */
  public void requireAuthenticated() {
    if (!authProperties.getEnforcement().isEnabled()) {
      return;
    }
    if (!TenantContext.isAuthenticated()) {
      throw new AccessDeniedException("Authentication required");
    }
  }

  /**
   * Verify the current user has ADMIN role.
   * When enforcement is disabled, this is a no-op.
   *
   * @throws AccessDeniedException if the current user is not an admin
   */
  public void requireAdmin() {
    if (!authProperties.getEnforcement().isEnabled()) {
      return;
    }
    var principal = TenantContext.getPrincipal();
    if (principal == null) {
      throw new AccessDeniedException("Authentication required");
    }
    if (!principal.isAdmin()) {
      throw new AccessDeniedException("Admin access required");
    }
  }
}
