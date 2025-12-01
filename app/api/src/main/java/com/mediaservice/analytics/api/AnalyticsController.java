package com.mediaservice.analytics.api;

import com.mediaservice.analytics.api.dto.*;
import com.mediaservice.analytics.application.AnalyticsService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.Parameter;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.responses.ApiResponses;
import io.swagger.v3.oas.annotations.tags.Tag;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;

/**
 * REST controller for analytics endpoints.
 *
 * <p>
 * Provides endpoints for querying view counts, leaderboards,
 * format usage, and download statistics.
 */
@Slf4j
@RestController
@RequestMapping("/v1/analytics")
@RequiredArgsConstructor
@Tag(name = "Analytics", description = "Analytics and statistics endpoints")
public class AnalyticsController {

  private final AnalyticsService analyticsService;

  @Operation(summary = "Get top viewed media", description = "Returns media items ranked by view count for the specified time period")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "Top media list") })
  @GetMapping("/top-media")
  public ResponseEntity<List<EntityViewCount>> getTopMedia(
      @Parameter(description = "Time period for the query") @RequestParam(defaultValue = "TODAY") Period period,
      @Parameter(description = "Maximum number of results to return") @RequestParam(defaultValue = "10") int limit) {
    log.debug("Getting top media for period: {}, limit: {}", period, limit);
    var topMedia = analyticsService.getTopMedia(period, Math.min(limit, 100));
    return ResponseEntity.ok(topMedia);
  }

  @Operation(summary = "Get view statistics for a media item", description = "Returns view counts across different time periods for a specific media")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "View statistics") })
  @GetMapping("/media/{mediaId}/views")
  public ResponseEntity<ViewStats> getMediaViews(@Parameter(description = "Media ID") @PathVariable String mediaId) {
    log.debug("Getting view stats for mediaId: {}", mediaId);
    var stats = analyticsService.getMediaViews(mediaId);
    return ResponseEntity.ok(stats);
  }

  @Operation(summary = "Get format usage statistics", description = "Returns breakdown of output format usage (JPEG, PNG, WebP)")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "Format usage statistics") })
  @GetMapping("/formats")
  public ResponseEntity<FormatUsageStats> getFormatUsage(
      @Parameter(description = "Time period for the query") @RequestParam(defaultValue = "TODAY") Period period) {
    log.debug("Getting format usage for period: {}", period);
    var stats = analyticsService.getFormatUsage(period);
    return ResponseEntity.ok(stats);
  }

  @Operation(summary = "Get download statistics", description = "Returns download counts with format and daily breakdowns")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "Download statistics") })
  @GetMapping("/downloads")
  public ResponseEntity<DownloadStats> getDownloadStats(
      @Parameter(description = "Time period for the query") @RequestParam(defaultValue = "TODAY") Period period) {
    log.debug("Getting download stats for period: {}", period);
    var stats = analyticsService.getDownloadStats(period);
    return ResponseEntity.ok(stats);
  }

  @Operation(summary = "Get analytics summary", description = "Returns comprehensive analytics overview for the dashboard")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "Analytics summary") })
  @GetMapping("/summary")
  public ResponseEntity<AnalyticsSummary> getSummary() {
    log.debug("Getting analytics summary");
    var summary = analyticsService.getSummary();
    return ResponseEntity.ok(summary);
  }
}
