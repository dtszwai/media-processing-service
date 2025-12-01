package com.mediaservice.lambda;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.RequestHandler;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.model.EventType;
import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.lambda.config.OpenTelemetryInitializer;
import com.mediaservice.lambda.service.AnalyticsDynamoDbService;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

import java.time.Instant;
import java.time.LocalDate;
import java.time.YearMonth;
import java.time.format.DateTimeFormatter;
import java.util.*;

/**
 * AWS Lambda handler for analytics rollup and archival operations.
 * Triggered by EventBridge scheduled rules.
 *
 * <p>
 * Implements the "Roll-Up Pattern" where:
 * <ul>
 * <li>API handles real-time writes to daily Redis keys</li>
 * <li>API handles write-behind persistence to DynamoDB every 5 minutes</li>
 * <li>This Lambda handles monthly aggregation and S3 archival</li>
 * </ul>
 *
 * <p>
 * Supported event types:
 * <ul>
 * <li>MONTHLY_ROLLUP - Aggregate last month's daily data into a monthly
 * summary</li>
 * <li>DAILY_ARCHIVE - Archive yesterday's daily data to S3</li>
 * <li>MONTHLY_ARCHIVE - Archive last month's data to S3</li>
 * </ul>
 *
 * <p>
 * Note: Daily/weekly/yearly data is calculated at read-time by summing
 * daily snapshots from DynamoDB, not stored as separate aggregations.
 */
public class AnalyticsRollupHandler implements RequestHandler<Map<String, Object>, String> {
  static {
    OpenTelemetryInitializer.initialize();
  }

  private static final Logger logger = LoggerFactory.getLogger(AnalyticsRollupHandler.class);

  private final AnalyticsDynamoDbService dynamoDbService;
  private final S3Client s3Client;
  private final ObjectMapper objectMapper;
  private final String bucketName;
  private final Tracer tracer;
  private final LongCounter monthlyRollupCounter;
  private final LongCounter dailyArchiveCounter;
  private final LongCounter monthlyArchiveCounter;
  private final LongCounter failureCounter;

  private static final DateTimeFormatter DATE_FORMATTER = DateTimeFormatter.ISO_LOCAL_DATE;
  private static final DateTimeFormatter MONTH_FORMATTER = DateTimeFormatter.ofPattern("yyyy-MM");

  /**
   * Default constructor for AWS Lambda runtime.
   */
  public AnalyticsRollupHandler() {
    this(new AnalyticsDynamoDbService(),
        com.mediaservice.lambda.config.AwsClientFactory.getS3Client(), new ObjectMapper());
  }

  /**
   * Constructor for testing with dependency injection.
   */
  AnalyticsRollupHandler(AnalyticsDynamoDbService dynamoDbService, S3Client s3Client, ObjectMapper objectMapper) {
    this.dynamoDbService = dynamoDbService;
    this.s3Client = s3Client;
    this.objectMapper = objectMapper;
    this.bucketName = LambdaConfig.getInstance().getBucketName();

    var otel = OpenTelemetryInitializer.initialize();
    this.tracer = otel.getTracer("media-service-analytics-lambda");
    var meter = otel.getMeter("media-service-analytics-lambda");

    this.monthlyRollupCounter = counter(meter, "lambda.analytics.monthly_rollup", "monthly rollup");
    this.dailyArchiveCounter = counter(meter, "lambda.analytics.daily_archive", "daily archive");
    this.monthlyArchiveCounter = counter(meter, "lambda.analytics.monthly_archive", "monthly archive");
    this.failureCounter = counter(meter, "lambda.analytics.failure", "failure");
  }

  private static LongCounter counter(Meter meter, String name, String desc) {
    return meter.counterBuilder(name).setDescription("Count of " + desc + " operations").build();
  }

  @Override
  public String handleRequest(Map<String, Object> event, Context context) {
    logger.info("AnalyticsRollup Lambda invoked with event: {}", event);

    var span = tracer.spanBuilder("analytics-rollup")
        .setSpanKind(SpanKind.INTERNAL)
        .startSpan();

    try {
      // Extract event type from EventBridge input
      String eventTypeStr = (String) event.get("type");
      if (eventTypeStr == null) {
        logger.warn("No event type specified in input");
        return "NO_EVENT_TYPE";
      }

      var eventType = EventType.fromString(eventTypeStr);
      if (eventType == null) {
        logger.warn("Unknown event type: {}", eventTypeStr);
        return "UNKNOWN_EVENT_TYPE";
      }

      span.setAttribute("rollup.type", eventType.getValue());

      switch (eventType) {
        case DAILY_ARCHIVE -> handleDailyArchive(span);
        case MONTHLY_ROLLUP -> handleMonthlyRollup(span);
        case MONTHLY_ARCHIVE -> handleMonthlyArchive(span);
        default -> {
          // Daily/weekly/yearly rollups are handled by API write-behind or calculated at
          // read-time
          logger.warn("Unhandled event type for analytics Lambda: {} (may be handled elsewhere)", eventType);
          return "UNHANDLED_EVENT_TYPE";
        }
      }

      span.setStatus(StatusCode.OK);
      return "OK";
    } catch (Exception e) {
      span.setStatus(StatusCode.ERROR, e.getMessage());
      span.recordException(e);
      failureCounter.add(1);
      logger.error("Analytics rollup failed: {}", e.getMessage(), e);
      throw e;
    } finally {
      span.end();
      OpenTelemetryInitializer.flush();
    }
  }

  /**
   * Handle daily archive - archive yesterday's daily data to S3.
   * This provides granular daily backups for long-term storage.
   */
  private void handleDailyArchive(Span span) {
    var yesterday = LocalDate.now().minusDays(1);
    String dateKey = yesterday.format(DATE_FORMATTER);
    String idempotencyKey = "daily-archive-" + dateKey;

    span.setAttribute("archive.date", dateKey);
    logger.info("Starting daily archive for {}", dateKey);

    if (!dynamoDbService.acquireIdempotencyLock(idempotencyKey)) {
      logger.info("Daily archive already processed for {}", dateKey);
      return;
    }

    // Get yesterday's daily data from DynamoDB
    var data = dynamoDbService.queryAnalytics("DAILY", dateKey);

    if (data.isEmpty()) {
      logger.info("No daily analytics data to archive for {}", dateKey);
      return;
    }

    // Archive to S3
    archiveDailyToS3(dateKey, data);
    dailyArchiveCounter.add(1);

    span.addEvent("daily-archive-complete",
        io.opentelemetry.api.common.Attributes.of(AttributeKey.longKey("media.count"), (long) data.size()));

    logger.info("Daily archive completed for {}: {} media items", dateKey, data.size());
  }

  /**
   * Handle monthly rollup - aggregate last month's daily data into a monthly
   * summary.
   * This creates a single MONTHLY record in DynamoDB for faster monthly queries.
   */
  private void handleMonthlyRollup(Span span) {
    var lastMonth = YearMonth.now().minusMonths(1);
    String monthKey = lastMonth.format(MONTH_FORMATTER);
    String idempotencyKey = "monthly-" + monthKey;

    span.setAttribute("rollup.period", monthKey);
    logger.info("Starting monthly rollup for {}", monthKey);

    if (!dynamoDbService.acquireIdempotencyLock(idempotencyKey)) {
      logger.info("Monthly rollup already processed for {}", monthKey);
      return;
    }

    // Aggregate from DynamoDB daily snapshots (populated by API write-behind)
    var dailyKeys = getDailyKeysForMonth(lastMonth);
    var data = dynamoDbService.aggregateAnalytics("DAILY", dailyKeys);

    if (data.isEmpty()) {
      logger.info("No monthly analytics data to rollup for {}", monthKey);
      return;
    }

    dynamoDbService.persistAnalytics("MONTHLY", monthKey, data);
    monthlyRollupCounter.add(1);

    span.addEvent("monthly-rollup-complete",
        io.opentelemetry.api.common.Attributes.of(
            AttributeKey.longKey("media.count"), (long) data.size()));

    logger.info("Monthly rollup completed for {}: {} media items", monthKey, data.size());
  }

  /**
   * Handle monthly archive - archive last month's data to S3.
   */
  private void handleMonthlyArchive(Span span) {
    var lastMonth = YearMonth.now().minusMonths(1);
    String monthKey = lastMonth.format(MONTH_FORMATTER);
    String idempotencyKey = "archive-" + monthKey;

    span.setAttribute("rollup.period", monthKey);
    logger.info("Starting monthly archive for {}", monthKey);

    if (!dynamoDbService.acquireIdempotencyLock(idempotencyKey)) {
      logger.info("Monthly archive already processed for {}", monthKey);
      return;
    }

    // Get monthly data from DynamoDB
    var data = dynamoDbService.queryAnalytics("MONTHLY", monthKey);

    if (data.isEmpty()) {
      logger.info("No monthly analytics data to archive for {}", monthKey);
      return;
    }

    // Archive to S3
    archiveMonthlyToS3(monthKey, data);
    monthlyArchiveCounter.add(1);

    span.addEvent("monthly-archive-complete",
        io.opentelemetry.api.common.Attributes.of(AttributeKey.longKey("media.count"), (long) data.size()));

    logger.info("Monthly archive completed for {}: {} media items", monthKey, data.size());
  }

  /**
   * Archive daily analytics data to S3.
   * Path: analytics/daily/{year}/{month}/analytics-{date}.json
   */
  private void archiveDailyToS3(String dateKey, Map<String, Long> data) {
    try {
      String[] parts = dateKey.split("-");
      String year = parts[0];
      String month = parts[1];
      String s3Key = String.format("analytics/daily/%s/%s/analytics-%s.json", year, month, dateKey);

      var archive = new AnalyticsArchive(
          dateKey,
          Instant.now().toString(),
          data.size(),
          data.values().stream().mapToLong(Long::longValue).sum(),
          data);

      String jsonContent = objectMapper.writeValueAsString(archive);

      var putRequest = PutObjectRequest.builder()
          .bucket(bucketName)
          .key(s3Key)
          .contentType("application/json")
          .build();

      s3Client.putObject(putRequest, RequestBody.fromString(jsonContent));

      logger.info("Archived daily analytics to S3: s3://{}/{}", bucketName, s3Key);
    } catch (Exception e) {
      logger.error("Failed to archive daily analytics to S3: {}", e.getMessage(), e);
      throw new RuntimeException("S3 daily archival failed", e);
    }
  }

  /**
   * Archive monthly analytics data to S3.
   * Path: analytics/monthly/{year}/{month}/analytics-{year-month}.json
   */
  private void archiveMonthlyToS3(String monthKey, Map<String, Long> data) {
    try {
      String[] parts = monthKey.split("-");
      String year = parts[0];
      String month = parts[1];
      String s3Key = String.format("analytics/monthly/%s/%s/analytics-%s.json", year, month, monthKey);

      var archive = new AnalyticsArchive(
          monthKey,
          Instant.now().toString(),
          data.size(),
          data.values().stream().mapToLong(Long::longValue).sum(),
          data);

      String jsonContent = objectMapper.writeValueAsString(archive);

      var putRequest = PutObjectRequest.builder()
          .bucket(bucketName)
          .key(s3Key)
          .contentType("application/json")
          .build();

      s3Client.putObject(putRequest, RequestBody.fromString(jsonContent));

      logger.info("Archived monthly analytics to S3: s3://{}/{}", bucketName, s3Key);
    } catch (Exception e) {
      logger.error("Failed to archive monthly analytics to S3: {}", e.getMessage(), e);
      throw new RuntimeException("S3 monthly archival failed", e);
    }
  }

  private List<String> getDailyKeysForMonth(YearMonth yearMonth) {
    var keys = new ArrayList<String>();
    var start = yearMonth.atDay(1);
    var end = yearMonth.atEndOfMonth();
    for (var date = start; !date.isAfter(end); date = date.plusDays(1)) {
      keys.add(date.format(DATE_FORMATTER));
    }
    return keys;
  }

  /**
   * Archive record for S3 storage.
   */
  public record AnalyticsArchive(
      String period,
      String archivedAt,
      int mediaCount,
      long totalViews,
      Map<String, Long> viewsByMedia) {
  }
}
