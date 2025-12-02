package com.mediaservice.media.api;

import com.mediaservice.shared.config.properties.MediaProperties;
import com.mediaservice.shared.http.error.ErrorResponse;
import com.mediaservice.shared.http.PagedResponse;
import com.mediaservice.media.api.dto.InitUploadRequest;
import com.mediaservice.media.api.dto.InitUploadResponse;
import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.media.api.dto.ResizeRequest;
import com.mediaservice.media.api.dto.StatusResponse;
import com.mediaservice.shared.http.error.MediaConflictException;
import com.mediaservice.shared.http.error.MediaGoneException;
import com.mediaservice.media.application.mapper.MediaMapper;
import com.mediaservice.media.application.MediaApplicationService;
import com.mediaservice.analytics.application.AnalyticsService;
import com.mediaservice.common.model.MediaStatus;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.responses.ApiResponses;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.net.URI;

/**
 * REST controller for media operations.
 *
 * <p>
 * Provides endpoints for uploading, downloading, resizing, and deleting media.
 */
@Slf4j
@RestController
@RequestMapping("/v1/media")
@RequiredArgsConstructor
@Tag(name = "Media", description = "Media upload and management endpoints")
public class MediaController {
  private final MediaApplicationService mediaService;
  private final MediaMapper mediaMapper;
  private final MediaProperties mediaProperties;
  private final AnalyticsService analyticsService;

  @GetMapping("/health")
  public ResponseEntity<String> health() {
    return ResponseEntity.ok("OK");
  }

  @Operation(summary = "List all media with pagination")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "List of media items") })
  @GetMapping
  public ResponseEntity<PagedResponse<MediaResponse>> getAllMedia(
      @RequestParam(required = false) String cursor,
      @RequestParam(required = false) Integer limit) {
    log.info("Get all media request: cursor={}, limit={}", cursor, limit);
    var result = mediaService.getMediaPaginated(cursor, limit);
    var items = result.items().stream().map(mediaMapper::toResponse).toList();
    return ResponseEntity.ok(PagedResponse.<MediaResponse>builder()
        .items(items)
        .nextCursor(result.nextCursor())
        .hasMore(result.hasMore())
        .build());
  }

  @Operation(summary = "Upload media file")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Upload accepted for processing"),
      @ApiResponse(responseCode = "400", description = "Invalid file")
  })
  @PostMapping("/upload")
  public ResponseEntity<MediaResponse> uploadMedia(
      @RequestParam("file") MultipartFile file,
      @RequestParam(required = false) Integer width,
      @RequestParam(required = false) String outputFormat) throws IOException {
    log.info("Upload request received: fileName={}, size={}, outputFormat={}",
        file.getOriginalFilename(), file.getSize(), outputFormat);
    validateUploadFile(file);
    return ResponseEntity.accepted().body(mediaService.uploadMedia(file, width, outputFormat));
  }

  @Operation(summary = "Initialize presigned upload")
  @ApiResponses({
      @ApiResponse(responseCode = "201", description = "Presigned upload initialized"),
      @ApiResponse(responseCode = "400", description = "Invalid request")
  })
  @PostMapping("/upload/init")
  public ResponseEntity<InitUploadResponse> initPresignedUpload(@Valid @RequestBody InitUploadRequest request) {
    log.info("Init presigned upload request: fileName={}, size={}, contentType={}",
        request.getFileName(), request.getFileSize(), request.getContentType());
    validatePresignedUploadRequest(request);
    return ResponseEntity.status(HttpStatus.CREATED).body(mediaService.initPresignedUpload(request));
  }

  @Operation(summary = "Complete presigned upload")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Upload completed", content = @Content(schema = @Schema(implementation = MediaResponse.class))),
      @ApiResponse(responseCode = "400", description = "Upload not found or invalid state", content = @Content(schema = @Schema(implementation = ErrorResponse.class)))
  })
  @PostMapping("/{mediaId}/upload/complete")
  public ResponseEntity<MediaResponse> completePresignedUpload(@PathVariable String mediaId) {
    log.info("Complete presigned upload request: mediaId={}", mediaId);
    return mediaService.completePresignedUpload(mediaId)
        .map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElseThrow(() -> new IllegalArgumentException(
            "Upload not found, not in PENDING_UPLOAD status, or file not uploaded to S3."));
  }

  @Operation(summary = "Refresh presigned upload URL", description = "Get a new presigned URL for a PENDING_UPLOAD media item")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "New upload URL generated"),
      @ApiResponse(responseCode = "404", description = "Media not found or not in PENDING_UPLOAD status")
  })
  @PostMapping("/{mediaId}/upload/refresh")
  public ResponseEntity<InitUploadResponse> refreshPresignedUploadUrl(@PathVariable String mediaId) {
    log.info("Refresh presigned upload URL request: mediaId={}", mediaId);
    return mediaService.refreshPresignedUploadUrl(mediaId)
        .map(ResponseEntity::ok)
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Get media by ID")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "Media found"),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "410", description = "Media has been deleted")
  })
  @GetMapping("/{mediaId}")
  public ResponseEntity<MediaResponse> getMedia(@PathVariable String mediaId) {
    log.info("Get media request: mediaId={}", mediaId);
    return mediaService.getMedia(mediaId)
        .<ResponseEntity<MediaResponse>>map(media -> {
          if (media.getStatus() == MediaStatus.DELETED) {
            throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
          }
          return ResponseEntity.ok(mediaMapper.toResponse(media));
        })
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Get media processing status")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "Status retrieved"),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "410", description = "Media has been deleted")
  })
  @GetMapping("/{mediaId}/status")
  public ResponseEntity<StatusResponse> getMediaStatus(@PathVariable String mediaId) {
    log.info("Status request: mediaId={}", mediaId);
    return mediaService.getMediaStatus(mediaId)
        .<ResponseEntity<StatusResponse>>map(status -> {
          if (status == MediaStatus.DELETED) {
            throw new MediaGoneException("Media has been deleted");
          }
          return ResponseEntity.ok(mediaMapper.toStatusResponse(status));
        })
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Download processed media", description = "Redirects to presigned S3 URL")
  @ApiResponses({
      @ApiResponse(responseCode = "302", description = "Redirect to download URL"),
      @ApiResponse(responseCode = "202", description = "Media still processing", content = @Content(schema = @Schema(implementation = MediaResponse.class))),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "410", description = "Media has been deleted")
  })
  @GetMapping("/{mediaId}/download")
  public ResponseEntity<MediaResponse> downloadMedia(@PathVariable String mediaId, HttpServletRequest request) {
    log.info("Download request: mediaId={}", mediaId);
    var mediaOpt = mediaService.getMedia(mediaId);
    if (mediaOpt.isEmpty()) {
      return ResponseEntity.notFound().build();
    }
    var media = mediaOpt.get();
    if (media.getStatus() == MediaStatus.DELETED) {
      throw new MediaGoneException("Media has been deleted", media.getDeletedAt());
    }
    if (mediaService.isMediaProcessing(mediaId)) {
      var headers = new HttpHeaders();
      headers.add("Retry-After", "60");
      headers.add("Location", "%s://%s:%d/v1/media/%s/status"
          .formatted(request.getScheme(), request.getServerName(), request.getServerPort(), mediaId));
      return ResponseEntity.accepted()
          .headers(headers)
          .body(mediaMapper.toMessageResponse("Media processing in progress."));
    }
    return mediaService.getDownloadUrl(mediaId)
        .<ResponseEntity<MediaResponse>>map(url -> {
          analyticsService.recordView(mediaId);
          analyticsService.recordDownload(mediaId, media.getOutputFormat(), media.getWidth());
          return ResponseEntity.status(HttpStatus.FOUND).location(URI.create(url)).build();
        })
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Resize media")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Resize request accepted", content = @Content(schema = @Schema(implementation = MediaResponse.class))),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "409", description = "Media not in COMPLETE status", content = @Content(schema = @Schema(implementation = ErrorResponse.class)))
  })
  @PutMapping("/{mediaId}/resize")
  public ResponseEntity<MediaResponse> resizeMedia(@PathVariable String mediaId,
      @Valid @RequestBody ResizeRequest resizeRequest) {
    log.info("Resize request: mediaId={}", mediaId);
    if (!mediaService.mediaExists(mediaId)) {
      return ResponseEntity.notFound().build();
    }
    return mediaService.resizeMedia(mediaId, resizeRequest.getWidth(), resizeRequest.getOutputFormat())
        .map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElseThrow(() -> new MediaConflictException("Cannot resize: media is not in COMPLETE status"));
  }

  @Operation(summary = "Delete media")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Delete request accepted"),
      @ApiResponse(responseCode = "404", description = "Media not found")
  })
  @DeleteMapping("/{mediaId}")
  public ResponseEntity<MediaResponse> deleteMedia(@PathVariable String mediaId) {
    log.info("Delete request: mediaId={}", mediaId);
    return mediaService.deleteMedia(mediaId)
        .<ResponseEntity<MediaResponse>>map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Retry processing", description = "Retry processing for media stuck in PROCESSING or ERROR status")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Retry initiated"),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "409", description = "Media not in retryable status (PROCESSING or ERROR)", content = @Content(schema = @Schema(implementation = ErrorResponse.class)))
  })
  @PostMapping("/{mediaId}/retry")
  public ResponseEntity<MediaResponse> retryProcessing(@PathVariable String mediaId) {
    log.info("Retry request: mediaId={}", mediaId);
    if (!mediaService.mediaExists(mediaId)) {
      return ResponseEntity.notFound().build();
    }
    return mediaService.retryProcessing(mediaId)
        .map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElseThrow(() -> new MediaConflictException("Cannot retry: media is not in PROCESSING or ERROR status"));
  }

  private void validateUploadFile(MultipartFile file) {
    long maxFileSize = mediaProperties.getMaxFileSize();
    if (file.getSize() > maxFileSize) {
      throw new IllegalArgumentException(
          "Failed to upload media. Check the file size. Max size is " + (maxFileSize / (1024 * 1024)) + " MB.");
    }
    if (file.isEmpty()) {
      throw new IllegalArgumentException("Malformed multipart form data.");
    }
  }

  private void validatePresignedUploadRequest(InitUploadRequest request) {
    long maxUploadSize = mediaProperties.getUpload().getMaxPresignedUploadSize();
    if (request.getFileSize() > maxUploadSize) {
      throw new IllegalArgumentException(
          "File size exceeds maximum allowed size of " + (maxUploadSize / (1024 * 1024 * 1024)) + " GB.");
    }
    if (!request.getContentType().startsWith("image/")) {
      throw new IllegalArgumentException("Invalid content type. Only images are supported.");
    }
  }
}
