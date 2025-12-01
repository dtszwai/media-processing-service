package com.mediaservice.analytics.api.dto;

import com.mediaservice.analytics.domain.model.EntityType;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

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
   * Create an EntityViewCount for media (backward compatible)
   */
  public static EntityViewCount forMedia(String mediaId, String name, long viewCount, int rank) {
    return EntityViewCount.builder()
        .entityType(EntityType.MEDIA)
        .entityId(mediaId)
        .name(name)
        .viewCount(viewCount)
        .rank(rank)
        .build();
  }
}
