package com.mediaservice.controller;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.dto.ErrorResponse;
import com.mediaservice.dto.InitUploadRequest;
import com.mediaservice.dto.InitUploadResponse;
import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.ResizeRequest;
import com.mediaservice.dto.StatusResponse;
import com.mediaservice.exception.MediaConflictException;
import com.mediaservice.mapper.MediaMapper;
import com.mediaservice.service.MediaService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.responses.ApiResponses;
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
import java.util.List;

@Slf4j
@RestController
@RequestMapping("/v1/media")
@RequiredArgsConstructor
public class MediaController {
  private final MediaService mediaService;
  private final MediaMapper mediaMapper;
  private final MediaProperties mediaProperties;

  @GetMapping("/health")
  public ResponseEntity<String> health() {
    return ResponseEntity.ok("OK");
  }

  @GetMapping
  public ResponseEntity<List<MediaResponse>> getAllMedia() {
    log.info("Get all media request");
    return ResponseEntity.ok(mediaService.getAllMedia().stream()
        .map(mediaMapper::toResponse)
        .toList());
  }

  @PostMapping("/upload")
  public ResponseEntity<MediaResponse> uploadMedia(
      @RequestParam("file") MultipartFile file,
      @RequestParam(value = "width", required = false) Integer width,
      @RequestParam(value = "outputFormat", required = false) String outputFormat) throws IOException {

    log.info("Upload request received: fileName={}, size={}, outputFormat={}",
        file.getOriginalFilename(), file.getSize(), outputFormat);

    validateUploadFile(file);
    return ResponseEntity.accepted().body(mediaService.uploadMedia(file, width, outputFormat));
  }

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

  @GetMapping("/{mediaId}")
  public ResponseEntity<MediaResponse> getMedia(@PathVariable String mediaId) {
    log.info("Get media request: mediaId={}", mediaId);
    return mediaService.getMedia(mediaId)
        .<ResponseEntity<MediaResponse>>map(media -> ResponseEntity.ok(mediaMapper.toResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }

  @GetMapping("/{mediaId}/status")
  public ResponseEntity<StatusResponse> getMediaStatus(@PathVariable String mediaId) {
    log.info("Status request: mediaId={}", mediaId);
    return mediaService.getMediaStatus(mediaId)
        .<ResponseEntity<StatusResponse>>map(status -> ResponseEntity.ok(mediaMapper.toStatusResponse(status)))
        .orElse(ResponseEntity.notFound().build());
  }

  @Operation(summary = "Download processed media", description = "Redirects to presigned S3 URL")
  @ApiResponses({
      @ApiResponse(responseCode = "302", description = "Redirect to download URL"),
      @ApiResponse(responseCode = "202", description = "Media still processing", content = @Content(schema = @Schema(implementation = MediaResponse.class))),
      @ApiResponse(responseCode = "404", description = "Media not found")
  })
  @GetMapping("/{mediaId}/download")
  public ResponseEntity<MediaResponse> downloadMedia(@PathVariable String mediaId, HttpServletRequest request) {
    log.info("Download request: mediaId={}", mediaId);
    if (!mediaService.mediaExists(mediaId)) {
      return ResponseEntity.notFound().build();
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
        .<ResponseEntity<MediaResponse>>map(
            url -> ResponseEntity.status(HttpStatus.FOUND).location(URI.create(url)).build())
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

  @DeleteMapping("/{mediaId}")
  public ResponseEntity<MediaResponse> deleteMedia(@PathVariable String mediaId) {
    log.info("Delete request: mediaId={}", mediaId);
    return mediaService.deleteMedia(mediaId)
        .<ResponseEntity<MediaResponse>>map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElse(ResponseEntity.notFound().build());
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
