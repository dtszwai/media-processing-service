package com.mediaservice.analytics.infrastructure;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.shared.config.properties.AnalyticsProperties;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

import java.time.Instant;
import java.util.Map;

/**
 * Service for archiving analytics data to S3.
 *
 * <p>S3 Archive Format:
 * <ul>
 *   <li>Path: {@code analytics/{tenantId}/{year}/{month}/analytics-{year}-{month}.json}</li>
 *   <li>Contains aggregated monthly data as JSON</li>
 * </ul>
 */
@Service
@Slf4j
public class AnalyticsS3ArchiveService {

    private final S3Client s3Client;
    private final ObjectMapper objectMapper;
    private final AnalyticsProperties analyticsProperties;
    private final String bucketName;

    public AnalyticsS3ArchiveService(
            S3Client s3Client,
            ObjectMapper objectMapper,
            AnalyticsProperties analyticsProperties,
            @Value("${aws.s3.bucket-name}") String bucketName) {
        this.s3Client = s3Client;
        this.objectMapper = objectMapper;
        this.analyticsProperties = analyticsProperties;
        this.bucketName = bucketName;
    }

    /**
     * Check if S3 archival is enabled.
     */
    public boolean isEnabled() {
        return analyticsProperties.isEnabled()
                && analyticsProperties.getPersistence().isEnabled()
                && analyticsProperties.getPersistence().getS3Archive().isEnabled();
    }

    /**
     * Archive analytics data to S3.
     *
     * @param yearMonth the year-month (e.g., "2024-01")
     * @param data      map of mediaId to view count
     */
    public void archive(String tenantId, String yearMonth, Map<String, Long> data) {
        if (!isEnabled()) {
            log.debug("S3 archival is disabled");
            return;
        }

        if (data.isEmpty()) {
            log.debug("No data to archive for {}", yearMonth);
            return;
        }

        try {
            String s3Key = buildS3Key(tenantId, yearMonth);
            AnalyticsArchive archiveRecord = createArchiveRecord(tenantId, yearMonth, data);
            String jsonContent = objectMapper.writeValueAsString(archiveRecord);

            var putRequest = PutObjectRequest.builder()
                    .bucket(bucketName)
                    .key(s3Key)
                    .contentType("application/json")
                    .build();

            s3Client.putObject(putRequest, RequestBody.fromString(jsonContent));

            log.info("Archived analytics for tenant {} on {} to S3: s3://{}/{} ({} media items, {} total views)",
                    tenantId, yearMonth, bucketName, s3Key, data.size(), archiveRecord.totalViews());

        } catch (Exception e) {
            log.error("Failed to archive analytics for tenant {} on {} to S3: {}", tenantId, yearMonth, e.getMessage(), e);
            throw new RuntimeException("S3 archival failed", e);
        }
    }

    private String buildS3Key(String tenantId, String yearMonth) {
        var s3Config = analyticsProperties.getPersistence().getS3Archive();
        String[] parts = yearMonth.split("-");
        String year = parts[0];
        String month = parts[1];
        return String.format("%s%s/%s/%s/analytics-%s.json", s3Config.getPrefix(), tenantId, year, month, yearMonth);
    }

    private AnalyticsArchive createArchiveRecord(String tenantId, String yearMonth, Map<String, Long> data) {
        long totalViews = data.values().stream().mapToLong(Long::longValue).sum();
        return new AnalyticsArchive(
                tenantId,
                yearMonth,
                Instant.now().toString(),
                data.size(),
                totalViews,
                data);
    }

    /**
     * Archive record for S3 storage.
     */
    public record AnalyticsArchive(
            String tenantId,
            String period,
            String archivedAt,
            int mediaCount,
            long totalViews,
            Map<String, Long> viewsByMedia) {
    }
}
