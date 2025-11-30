package com.mediaservice.config;

import lombok.Getter;
import lombok.Setter;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

/**
 * Configuration properties for analytics features.
 *
 * <p>
 * Controls analytics behavior including enabling/disabling the feature,
 * default limits for leaderboards, and data retention settings.
 */
@Component
@ConfigurationProperties(prefix = "analytics")
@Getter
@Setter
public class AnalyticsProperties {
  private boolean enabled = true;
  /**
   * Default limit for top media leaderboards.
   */
  private int topMediaLimit = 10;

  /**
   * Number of days to retain analytics data.
   */
  private int retentionDays = 90;
}
