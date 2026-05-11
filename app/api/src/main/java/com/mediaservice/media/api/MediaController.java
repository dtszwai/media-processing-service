package com.mediaservice.media.api;

import com.mediaservice.media.api.dto.AssetDownloadUrlResponse;
import com.mediaservice.media.api.dto.CreateAssetRequest;
import com.mediaservice.media.api.dto.InitUploadRequest;
import com.mediaservice.media.api.dto.InitUploadResponse;
import com.mediaservice.media.api.dto.MediaAssetResponse;
import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.media.application.MediaApplicationService;
import com.mediaservice.media.application.mapper.MediaMapper;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.MediaSource;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.shared.http.PagedResponse;
import com.mediaservice.shared.http.error.ErrorResponse;
import com.mediaservice.shared.idempotency.Idempotent;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.responses.ApiResponses;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.validation.Valid;
import java.net.URI;
import java.util.List;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;

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
      @RequestParam(required = false) String mediaType,
      @RequestParam(required = false) String source) {
    MediaType resolvedType = MediaType.fromString(mediaType);
    if (mediaType != null && resolvedType == null) {
      throw new IllegalArgumentException("Invalid mediaType. Supported values: image, document, video, audio, other.");
    }
    MediaSource resolvedSource = source != null ? MediaSource.fromString(source) : null;
    log.info("Get all media request: cursor={}, limit={}, mediaType={}, source={}", cursor, limit,
        resolvedType != null ? resolvedType.getValue() : "any",
        resolvedSource != null ? resolvedSource.getValue() : "any");
    var result = mediaService.getMediaPaginated(cursor, limit, resolvedType, resolvedSource);
    var thumbnailUrls = mediaService.getThumbnailUrls(result.items());
    var items = result.items().stream()
        .map(m -> mediaMapper.toResponse(m, thumbnailUrls.get(m.getMediaId())))
        .toList();
    return ResponseEntity.ok(PagedResponse.<MediaResponse>builder()
        .items(items)
        .nextCursor(result.nextCursor())
        .hasMore(result.hasMore())
        .build());
  }

  @Operation(summary = "Upload media file", description = "Upload a media file. Supports idempotency via Idempotency-Key header.")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Upload accepted"),
      @ApiResponse(responseCode = "400", description = "Invalid file")
  })
  @Idempotent(scope = "upload")
  @PostMapping("/upload")
  public ResponseEntity<MediaResponse> uploadMedia(
      @RequestParam("file") MultipartFile file,
      @RequestParam(required = false) String mediaType) throws Exception {
    log.info("Upload request received: fileName={}, size={}, mediaType={}",
        file.getOriginalFilename(), file.getSize(), mediaType);

    mediaService.validateUploadFile(file.getSize(), file.isEmpty());
    var media = mediaService.uploadMedia(file, mediaType);
    return ResponseEntity.accepted().body(mediaMapper.toResponse(media));
  }

  @Operation(summary = "Initialize presigned upload", description = "Initialize a presigned upload. Supports idempotency via Idempotency-Key header.")
  @ApiResponses({
      @ApiResponse(responseCode = "201", description = "Presigned upload initialized"),
      @ApiResponse(responseCode = "400", description = "Invalid request")
  })
  @Idempotent(scope = "init-upload")
  @PostMapping("/upload/init")
  public ResponseEntity<InitUploadResponse> initPresignedUpload(
      @Valid @RequestBody InitUploadRequest request) {
    log.info("Init presigned upload request: fileName={}, size={}, contentType={}",
        request.getFileName(), request.getFileSize(), request.getContentType());

    mediaService.validatePresignedUploadRequest(request.getFileSize(), request.getContentType(),
        request.getMediaType(), request.getFileName());
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
      @ApiResponse(responseCode = "404", description = "Media not found")
  })
  @GetMapping("/{mediaId}")
  public ResponseEntity<MediaResponse> getMedia(@PathVariable String mediaId) {
    log.info("Get media request: mediaId={}", mediaId);
    return mediaService.getActiveMedia(mediaId)
        .map(media -> ResponseEntity.ok(mediaMapper.toResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "List assets for a media item")
  @ApiResponses({ @ApiResponse(responseCode = "200", description = "List of assets") })
  @GetMapping("/{mediaId}/assets")
  public ResponseEntity<List<MediaAssetResponse>> listAssets(@PathVariable String mediaId) {
    log.info("List assets request: mediaId={}", mediaId);
    var assets = mediaService.listAssets(mediaId).stream().map(mediaMapper::toAssetResponse).toList();
    return ResponseEntity.ok(assets);
  }

  @Operation(summary = "Get asset by ID")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "Asset found"),
      @ApiResponse(responseCode = "404", description = "Asset not found")
  })
  @GetMapping("/{mediaId}/assets/{assetId}")
  public ResponseEntity<MediaAssetResponse> getAsset(@PathVariable String mediaId, @PathVariable String assetId) {
    log.info("Get asset request: mediaId={}, assetId={}", mediaId, assetId);
    return mediaService.getAsset(mediaId, assetId)
        .map(asset -> ResponseEntity.ok(mediaMapper.toAssetResponse(asset)))
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Get presigned download URL for an asset")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "Download URL generated"),
      @ApiResponse(responseCode = "202", description = "Asset still processing"),
      @ApiResponse(responseCode = "404", description = "Asset not found")
  })
  @GetMapping("/{mediaId}/assets/{assetId}/download-url")
  public ResponseEntity<AssetDownloadUrlResponse> getAssetDownloadUrl(
      @PathVariable String mediaId,
      @PathVariable String assetId) {
    log.info("Get asset download URL request: mediaId={}, assetId={}", mediaId, assetId);
    var assetOpt = mediaService.getAsset(mediaId, assetId);
    if (assetOpt.isEmpty()) {
      return ResponseEntity.notFound().build();
    }
    var asset = assetOpt.get();
    if (!isAssetComplete(asset)) {
      return ResponseEntity.status(HttpStatus.ACCEPTED).build();
    }
    return mediaService.getAssetDownloadUrl(mediaId, assetId)
        .map(url -> ResponseEntity.ok(AssetDownloadUrlResponse.builder().url(url).build()))
        .orElse(ResponseEntity.status(HttpStatus.ACCEPTED).build());
  }

  @Operation(summary = "Get presigned inline preview URL for an asset")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "Preview URL generated"),
      @ApiResponse(responseCode = "202", description = "Asset still processing"),
      @ApiResponse(responseCode = "404", description = "Asset not found")
  })
  @GetMapping("/{mediaId}/assets/{assetId}/preview-url")
  public ResponseEntity<AssetDownloadUrlResponse> getAssetPreviewUrl(
      @PathVariable String mediaId,
      @PathVariable String assetId) {
    log.info("Get asset preview URL request: mediaId={}, assetId={}", mediaId, assetId);
    var assetOpt = mediaService.getAsset(mediaId, assetId);
    if (assetOpt.isEmpty()) {
      return ResponseEntity.notFound().build();
    }
    var asset = assetOpt.get();
    if (!isAssetComplete(asset)) {
      return ResponseEntity.status(HttpStatus.ACCEPTED).build();
    }
    return mediaService.getAssetPreviewUrl(mediaId, assetId)
        .map(url -> ResponseEntity.ok(AssetDownloadUrlResponse.builder().url(url).build()))
        .orElse(ResponseEntity.status(HttpStatus.ACCEPTED).build());
  }

  @Operation(summary = "Create derived assets from a source asset")
  @ApiResponses({ @ApiResponse(responseCode = "202", description = "Assets queued for processing") })
  @PostMapping("/{mediaId}/assets")
  public ResponseEntity<List<MediaAssetResponse>> createAssets(
      @PathVariable String mediaId,
      @Valid @RequestBody CreateAssetRequest request) {
    log.info("Create assets request: mediaId={}, outputs={}", mediaId, request.getOutputs().size());
    var assets = mediaService.createAssets(mediaId, request).stream().map(mediaMapper::toAssetResponse).toList();
    return ResponseEntity.accepted().body(assets);
  }

  @Operation(summary = "Download asset", description = "Redirects to presigned S3 URL for asset")
  @ApiResponses({
      @ApiResponse(responseCode = "302", description = "Redirect to download URL"),
      @ApiResponse(responseCode = "202", description = "Asset still processing"),
      @ApiResponse(responseCode = "404", description = "Asset not found")
  })
  @GetMapping("/{mediaId}/assets/{assetId}/download")
  public ResponseEntity<?> downloadAsset(@PathVariable String mediaId, @PathVariable String assetId, HttpServletRequest request) {
    log.info("Download asset request: mediaId={}, assetId={}", mediaId, assetId);
    var assetOpt = mediaService.getAsset(mediaId, assetId);
    if (assetOpt.isEmpty()) {
      return ResponseEntity.notFound().build();
    }
    var asset = assetOpt.get();
    if (!isAssetComplete(asset)) {
      return acceptedAssetPendingResponse(request, mediaId, assetId);
    }
    return mediaService.getAssetDownloadUrl(mediaId, assetId)
        .map(url -> ResponseEntity.status(HttpStatus.FOUND).location(URI.create(url)).build())
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Retry processing for a failed asset")
  @ApiResponses({
      @ApiResponse(responseCode = "202", description = "Retry queued"),
      @ApiResponse(responseCode = "404", description = "Asset not found")
  })
  @PostMapping("/{mediaId}/assets/{assetId}/retry")
  public ResponseEntity<MediaAssetResponse> retryAsset(@PathVariable String mediaId, @PathVariable String assetId) {
    log.info("Retry asset request: mediaId={}, assetId={}", mediaId, assetId);
    return mediaService.retryAsset(mediaId, assetId)
        .map(asset -> ResponseEntity.accepted().body(mediaMapper.toAssetResponse(asset)))
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Delete media by ID")
  @ApiResponses({
      @ApiResponse(responseCode = "200", description = "Media deleted"),
      @ApiResponse(responseCode = "404", description = "Media not found")
  })
  @DeleteMapping("/{mediaId}")
  public ResponseEntity<MediaResponse> deleteMedia(@PathVariable String mediaId) {
    log.info("Delete media request: mediaId={}", mediaId);
    return mediaService.deleteMedia(mediaId)
        .map(media -> ResponseEntity.ok(mediaMapper.toResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }

  private boolean isAssetComplete(MediaAsset asset) {
    return asset.getStatus() == AssetStatus.COMPLETE;
  }

  private ResponseEntity<Void> acceptedAssetPendingResponse(HttpServletRequest request, String mediaId, String assetId) {
    var headers = new HttpHeaders();
    headers.add("Retry-After", "60");
    headers.add("Location", "%s://%s:%d/v1/media/%s/assets/%s"
        .formatted(request.getScheme(), request.getServerName(), request.getServerPort(), mediaId, assetId));
    return ResponseEntity.accepted().headers(headers).build();
  }
}
