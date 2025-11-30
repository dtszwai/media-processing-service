package com.mediaservice.dto.analytics;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.util.Map;

/**
 * Download statistics including totals and breakdowns.
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class DownloadStats {
  private Period period;
  private long totalDownloads;
  private Map<String, Long> byFormat;
  private Map<String, Long> byDay;
}
