package com.mediaservice.dto.analytics;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.util.List;
import java.util.Map;

/**
 * Summary of all analytics metrics for the dashboard.
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class AnalyticsSummary {
  private long totalViews;
  private long totalDownloads;
  private long viewsToday;
  private long downloadsToday;
  private List<MediaViewCount> topMediaToday;
  private List<MediaViewCount> topMediaAllTime;
  private Map<String, Long> formatUsage;
}
