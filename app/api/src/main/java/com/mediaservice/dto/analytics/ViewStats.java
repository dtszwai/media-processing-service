package com.mediaservice.dto.analytics;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * View statistics for a specific media item across different time periods.
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class ViewStats {
  private String mediaId;
  private long total;
  private long today;
  private long thisWeek;
  private long thisMonth;
  private long thisYear;
}
