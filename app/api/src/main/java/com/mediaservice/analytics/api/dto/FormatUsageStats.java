package com.mediaservice.analytics.api.dto;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.util.Map;

/**
 * Statistics on output format usage (JPEG, PNG, WebP).
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class FormatUsageStats {
  private Period period;
  private Map<String, Long> usage;
  private long total;
}
