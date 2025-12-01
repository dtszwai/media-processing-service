package com.mediaservice.analytics.domain.model;

/**
 * Represents the type of entity being tracked for analytics.
 *
 * <p>
 * This enum enables the analytics system to be generic and track
 * views/downloads for different entity types, not just media.
 *
 * <p>
 * Future entity types can be added here as the application grows
 * (e.g., THREAD, COMMENT, USER_PROFILE).
 */
public enum EntityType {
  /**
   * Media files (images, videos, etc.)
   */
  MEDIA("media"),

  /**
   * Thread/discussion posts (future use)
   */
  THREAD("thread"),

  /**
   * Comments on threads or media (future use)
   */
  COMMENT("comment"),

  /**
   * User profiles (future use)
   */
  USER("user");

  private final String prefix;

  EntityType(String prefix) {
    this.prefix = prefix;
  }

  /**
   * Get the Redis key prefix for this entity type.
   *
   * @return the key prefix
   */
  public String getPrefix() {
    return prefix;
  }

  /**
   * Build a Redis key for this entity type.
   *
   * @param suffix the key suffix (e.g., "views:daily:2024-01-15")
   * @return the full Redis key
   */
  public String buildKey(String suffix) {
    return prefix + ":" + suffix;
  }
}
