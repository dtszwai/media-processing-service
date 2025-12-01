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

  /**
   * Persistence configuration for snapshotting Redis data to DynamoDB.
   */
  private PersistenceConfig persistence = new PersistenceConfig();

  @Getter
  @Setter
  public static class PersistenceConfig {
    private boolean enabled = true;

    /**
     * Cron expression for daily snapshot job.
     * Default: 1:00 AM every day.
     */
    private String snapshotCron = "0 0 1 * * *";

    /**
     * Whether to enable TTL-based automatic deletion in DynamoDB.
     * When false, data is kept permanently in DynamoDB.
     * Default: false (keep data permanently).
     */
    private boolean ttlEnabled = false;

    /**
     * Retention period for daily snapshots in DynamoDB (days).
     * Only used when ttlEnabled is true.
     */
    private int dailyRetentionDays = 90;

    /**
     * Retention period for monthly snapshots in DynamoDB (days).
     * Only used when ttlEnabled is true.
     */
    private int monthlyRetentionDays = 730;

    /**
     * Retention period for yearly snapshots in DynamoDB (days).
     * Only used when ttlEnabled is true.
     */
    private int yearlyRetentionDays = 1825;

    /**
     * S3 archival configuration for long-term storage.
     */
    private S3ArchiveConfig s3Archive = new S3ArchiveConfig();
  }

  @Getter
  @Setter
  public static class S3ArchiveConfig {
    /**
     * Whether to enable S3 archival for analytics data.
     */
    private boolean enabled = true;

    /**
     * S3 prefix for analytics archive files.
     */
    private String prefix = "analytics/";

    /**
     * Cron expression for monthly archival job.
     * Default: 2:00 AM on the 1st of each month.
     */
    private String archiveCron = "0 0 2 1 * *";
  }
}
