package com.mediaservice.analytics.api.dto;

import com.mediaservice.analytics.domain.model.EntityType;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * View statistics for a specific entity across different time periods.
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class ViewStats {
  private EntityType entityType;
  private String entityId;
  private long total;
  private long today;
  private long thisWeek;
  private long thisMonth;
  private long thisYear;

  /**
   * Create ViewStats for media (backward compatible)
   */
  public static ViewStats forMedia(String mediaId, long total, long today, long thisWeek, long thisMonth, long thisYear) {
    return ViewStats.builder()
        .entityType(EntityType.MEDIA)
        .entityId(mediaId)
        .total(total)
        .today(today)
        .thisWeek(thisWeek)
        .thisMonth(thisMonth)
        .thisYear(thisYear)
        .build();
  }
}
