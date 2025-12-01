package com.mediaservice.analytics.api.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.mediaservice.analytics.domain.model.EntityType;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.Instant;

/**
 * View count information for any entity (media, thread, comment, etc.).
 *
 * <p>
 * This is a generic DTO that can represent view counts for any entity type,
 * making the analytics system extensible for future features.
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
@JsonInclude(JsonInclude.Include.NON_NULL)
public class EntityViewCount {
  /**
   * The type of entity (MEDIA, THREAD, etc.)
   */
  private EntityType entityType;

  /**
   * The unique identifier of the entity
   */
  private String entityId;

  /**
   * Human-readable name of the entity
   */
  private String name;

  /**
   * Total view count for this entity
   */
  private long viewCount;

  /**
   * Rank position in the leaderboard
   */
  private int rank;

  /**
   * Whether this entity has been soft deleted
   */
  private boolean deleted;

  /**
   * When this entity was deleted (null if not deleted)
   */
  private Instant deletedAt;

  /**
   * Create an EntityViewCount for media (backward compatible)
   */
  public static EntityViewCount forMedia(String mediaId, String name, long viewCount, int rank) {
    return EntityViewCount.builder()
        .entityType(EntityType.MEDIA)
        .entityId(mediaId)
        .name(name)
        .viewCount(viewCount)
        .rank(rank)
        .deleted(false)
        .build();
  }

  /**
   * Create an EntityViewCount for media with deletion status
   */
  public static EntityViewCount forMedia(String mediaId, String name, long viewCount, int rank,
      boolean deleted, Instant deletedAt) {
    return EntityViewCount.builder()
        .entityType(EntityType.MEDIA)
        .entityId(mediaId)
        .name(name)
        .viewCount(viewCount)
        .rank(rank)
        .deleted(deleted)
        .deletedAt(deletedAt)
        .build();
  }
}
