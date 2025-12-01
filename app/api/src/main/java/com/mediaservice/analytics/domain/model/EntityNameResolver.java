package com.mediaservice.analytics.domain.model;

import java.util.Optional;

/**
 * Interface for resolving entity names from entity IDs.
 *
 * <p>
 * This allows the analytics domain to look up human-readable names
 * for entities without depending directly on specific repositories.
 * Each domain (media, thread, etc.) can provide its own implementation.
 */
public interface EntityNameResolver {

  /**
   * Resolve the name of an entity given its ID.
   *
   * @param entityType The type of entity
   * @param entityId   The unique identifier of the entity
   * @return The entity's human-readable name, or empty if not found
   */
  Optional<String> resolveName(EntityType entityType, String entityId);

  /**
   * Check if this resolver supports the given entity type.
   *
   * @param entityType The entity type to check
   * @return true if this resolver can resolve names for the entity type
   */
  boolean supports(EntityType entityType);
}
