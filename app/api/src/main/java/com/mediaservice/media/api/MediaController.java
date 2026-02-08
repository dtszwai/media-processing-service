package com.mediaservice.media.api;

import com.mediaservice.shared.http.error.ErrorResponse;
import com.mediaservice.shared.http.error.MediaConflictException;
import com.mediaservice.shared.http.error.MediaGoneException;
import com.mediaservice.shared.http.PagedResponse;
import com.mediaservice.shared.idempotency.Idempotent;
import com.mediaservice.media.api.dto.InitUploadRequest;
import com.mediaservice.media.api.dto.InitUploadResponse;
import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.media.api.dto.ResizeRequest;
import com.mediaservice.media.api.dto.StatusResponse;
import com.mediaservice.media.application.DownloadResult;
import com.mediaservice.media.application.MediaOperationResult;
import com.mediaservice.media.application.PreviewResult;
import com.mediaservice.media.application.mapper.MediaMapper;
import com.mediaservice.media.application.MediaApplicationService;
import com.mediaservice.common.model.MediaType;
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

  @GetMapping("/health")
  public ResponseEntity<String> health() {
    return ResponseEntity.ok("OK");
  }

  @Operation(summary = "List all media with pagination")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "List of media items") })
  @GetMapping
  public ResponseEntity<PagedResponse<MediaResponse>> getAllMedia(
      @RequestParam(required = false) String cursor,
      @RequestParam(required = false) Integer limit,
      @RequestParam(required = false) String mediaType) {
    MediaType resolvedType = MediaType.fromString(mediaType);
    if (mediaType != null && resolvedType == null) {
      throw new IllegalArgumentException("Invalid mediaType. Supported values: image, document, video, audio, other.");
    }
    log.info("Get all media request: cursor={}, limit={}, mediaType={}", cursor, limit,
        resolvedType != null ? resolvedType.getValue() : "any");
    var result = mediaService.getMediaPaginated(cursor, limit, resolvedType);
    var items = result.items().stream().map(mediaMapper::toResponse).toList();
    return ResponseEntity.ok(PagedResponse.<MediaResponse>builder()
        .items(items)
        .nextCursor(result.nextCursor())
        .hasMore(result.hasMore())
        .build());
  }

  @Operation(summary = "Upload media file", description = "Upload a media file for processing. Supports idempotency via Idempotency-Key header.")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Upload accepted for processing"),
      @ApiResponse(responseCode = "400", description = "Invalid file"),
      @ApiResponse(responseCode = "409", description = "Request with same idempotency key already in progress")
  })
  @Idempotent(scope = "upload")
  @PostMapping("/upload")
  public ResponseEntity<MediaResponse> uploadMedia(
      @RequestParam("file") MultipartFile file,
      @RequestParam(required = false) Integer width,
      @RequestParam(required = false) String outputFormat,
      @RequestParam(required = false) String mediaType) throws IOException {
    log.info("Upload request received: fileName={}, size={}, outputFormat={}, mediaType={}",
        file.getOriginalFilename(), file.getSize(), outputFormat, mediaType);

    mediaService.validateUploadFile(file.getSize(), file.isEmpty());
    MediaResponse response = mediaService.uploadMedia(file, width, outputFormat, mediaType);
    return ResponseEntity.accepted().body(response);
  }

  @Operation(summary = "Initialize presigned upload", description = "Initialize a presigned upload. Supports idempotency via Idempotency-Key header.")
  @ApiResponses({
      @ApiResponse(responseCode = "201", description = "Presigned upload initialized"),
      @ApiResponse(responseCode = "400", description = "Invalid request"),
      @ApiResponse(responseCode = "409", description = "Request with same idempotency key already in progress")
  })
  @Idempotent(scope = "init-upload")
  @PostMapping("/upload/init")
  public ResponseEntity<InitUploadResponse> initPresignedUpload(
      @Valid @RequestBody InitUploadRequest request) {
    log.info("Init presigned upload request: fileName={}, size={}, contentType={}",
        request.getFileName(), request.getFileSize(), request.getContentType());

    mediaService.validatePresignedUploadRequest(request.getFileSize(), request.getContentType(), request.getMediaType(), request.getFileName());
    InitUploadResponse response = mediaService.initPresignedUpload(request);
    return ResponseEntity.status(HttpStatus.CREATED).body(response);
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
    return mediaService.getActiveMedia(mediaId)
        .map(media -> ResponseEntity.ok(mediaMapper.toResponse(media)))
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
    return mediaService.getActiveMediaStatus(mediaId)
        .map(status -> ResponseEntity.ok(mediaMapper.toStatusResponse(status)))
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Get original image", description = "Redirects to presigned S3 URL for original uploaded file")
  @ApiResponses({
      @ApiResponse(responseCode = "302", description = "Redirect to original file URL"),
      @ApiResponse(responseCode = "404", description = "Media not found or not yet uploaded"),
      @ApiResponse(responseCode = "410", description = "Media has been deleted")
  })
  @GetMapping("/{mediaId}/original")
  public ResponseEntity<Void> getOriginalUrl(@PathVariable String mediaId) {
    log.info("Original URL request: mediaId={}", mediaId);

    return mediaService.getOriginalUrl(mediaId)
        .map(url -> ResponseEntity.status(HttpStatus.FOUND).location(URI.create(url)).<Void>build())
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Get preview image", description = "Redirects to CDN URL for watermarked preview image")
  @ApiResponses({
      @ApiResponse(responseCode = "302", description = "Redirect to preview URL"),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "202", description = "Media still processing")
  })
  @GetMapping("/{mediaId}/preview")
  public ResponseEntity<Void> getPreviewUrl(@PathVariable String mediaId) {
    log.info("Preview URL request: mediaId={}", mediaId);

    return switch (mediaService.preparePreview(mediaId)) {
      case PreviewResult.Ready ready ->
          ResponseEntity.status(HttpStatus.FOUND).location(URI.create(ready.url())).build();
      case PreviewResult.Processing ignored ->
          ResponseEntity.accepted().build();
      case PreviewResult.NotFound ignored ->
          ResponseEntity.notFound().build();
    };
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

    return switch (mediaService.prepareDownload(mediaId)) {
      case DownloadResult.Ready ready ->
          ResponseEntity.status(HttpStatus.FOUND).location(URI.create(ready.url())).build();
      case DownloadResult.Processing processing -> {
        var headers = new HttpHeaders();
        headers.add("Retry-After", "60");
        headers.add("Location", "%s://%s:%d/v1/media/%s/status"
            .formatted(request.getScheme(), request.getServerName(), request.getServerPort(), processing.mediaId()));
        yield ResponseEntity.accepted()
            .headers(headers)
            .body(mediaMapper.toMessageResponse("Media processing in progress."));
      }
      case DownloadResult.NotFound ignored -> ResponseEntity.notFound().build();
    };
  }

  @Operation(summary = "Resize media")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Resize request accepted", content = @Content(schema = @Schema(implementation = MediaResponse.class))),
      @ApiResponse(responseCode = "404", description = "Media not found"),
      @ApiResponse(responseCode = "409", description = "Media not in COMPLETE status", content = @Content(schema = @Schema(implementation = ErrorResponse.class))),
      @ApiResponse(responseCode = "410", description = "Media has been deleted")
  })
  @PutMapping("/{mediaId}/resize")
  public ResponseEntity<MediaResponse> resizeMedia(@PathVariable String mediaId,
      @Valid @RequestBody ResizeRequest resizeRequest) {
    log.info("Resize request: mediaId={}", mediaId);

    return switch (mediaService.resizeMedia(mediaId, resizeRequest.getWidth(), resizeRequest.getOutputFormat())) {
      case MediaOperationResult.Success success ->
          ResponseEntity.accepted().body(mediaMapper.toIdResponse(success.media()));
      case MediaOperationResult.NotFound ignored ->
          ResponseEntity.notFound().build();
      case MediaOperationResult.Deleted deleted ->
          throw new MediaGoneException("Media has been deleted", deleted.deletedAt());
      case MediaOperationResult.NotAllowed notAllowed ->
          throw new MediaConflictException(notAllowed.reason());
    };
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
      @ApiResponse(responseCode = "409", description = "Media not in retryable status (PROCESSING or ERROR)", content = @Content(schema = @Schema(implementation = ErrorResponse.class))),
      @ApiResponse(responseCode = "410", description = "Media has been deleted")
  })
  @PostMapping("/{mediaId}/retry")
  public ResponseEntity<MediaResponse> retryProcessing(@PathVariable String mediaId) {
    log.info("Retry request: mediaId={}", mediaId);

    return switch (mediaService.retryProcessing(mediaId)) {
      case MediaOperationResult.Success success ->
          ResponseEntity.accepted().body(mediaMapper.toIdResponse(success.media()));
      case MediaOperationResult.NotFound ignored ->
          ResponseEntity.notFound().build();
      case MediaOperationResult.Deleted deleted ->
          throw new MediaGoneException("Media has been deleted", deleted.deletedAt());
      case MediaOperationResult.NotAllowed notAllowed ->
          throw new MediaConflictException(notAllowed.reason());
    };
  }
}
