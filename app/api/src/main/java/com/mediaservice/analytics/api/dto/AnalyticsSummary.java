package com.mediaservice.analytics.api.dto;

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
  private List<EntityViewCount> topMediaToday;
  private List<EntityViewCount> topMediaAllTime;
  private Map<String, Long> formatUsage;
}
