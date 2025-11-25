package com.mediaservice.controller;

import com.mediaservice.dto.ErrorResponse;
import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.ResizeRequest;
import com.mediaservice.service.MediaService;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;
import java.net.URI;

@Slf4j
@RestController
@RequestMapping("/v1/media")
@RequiredArgsConstructor
public class MediaController {

  private final MediaService mediaService;

  @Value("${media.max-file-size}")
  private long maxFileSize;

  @GetMapping("/health")
  public ResponseEntity<String> health() {
    return ResponseEntity.ok("OK");
  }

  @PostMapping("/upload")
  public ResponseEntity<?> uploadMedia(
      @RequestParam("file") MultipartFile file,
      @RequestParam(value = "width", required = false) Integer width) {

    log.info("Upload request received: fileName={}, size={}",
        file.getOriginalFilename(), file.getSize());

    // Validate file size
    if (file.getSize() > maxFileSize) {
      long maxSizeMb = maxFileSize / (1024 * 1024);
      return ResponseEntity.badRequest()
          .body(ErrorResponse.builder()
              .message("Failed to upload media. Check the file size. Max size is "
                  + maxSizeMb + " MB.")
              .status(400)
              .build());
    }

    // Validate file is not empty
    if (file.isEmpty()) {
      return ResponseEntity.badRequest()
          .body(ErrorResponse.builder()
              .message("Malformed multipart form data.")
              .status(400)
              .build());
    }

    try {
      MediaResponse response = mediaService.uploadMedia(file, width);
      return ResponseEntity.accepted().body(response);
    } catch (IllegalArgumentException e) {
      return ResponseEntity.badRequest()
          .body(ErrorResponse.builder()
              .message(e.getMessage())
              .status(400)
              .build());
    } catch (IOException e) {
      log.error("Failed to upload media: {}", e.getMessage());
      return ResponseEntity.internalServerError()
          .body(ErrorResponse.builder()
              .message("Internal server error")
              .status(500)
              .build());
    }
  }

  @GetMapping("/{mediaId}")
  public ResponseEntity<?> getMedia(@PathVariable String mediaId) {
    log.info("Get media request: mediaId={}", mediaId);

    return mediaService.getMedia(mediaId)
        .<ResponseEntity<?>>map(ResponseEntity::ok)
        .orElse(ResponseEntity.notFound().build());
  }

  @GetMapping("/{mediaId}/status")
  public ResponseEntity<?> getMediaStatus(@PathVariable String mediaId) {
    log.info("Status request: mediaId={}", mediaId);

    return mediaService.getMediaStatus(mediaId)
        .<ResponseEntity<?>>map(ResponseEntity::ok)
        .orElse(ResponseEntity.notFound().build());
  }

  @GetMapping("/{mediaId}/download")
  public ResponseEntity<?> downloadMedia(
      @PathVariable String mediaId,
      HttpServletRequest request) {

    log.info("Download request: mediaId={}", mediaId);

    if (!mediaService.mediaExists(mediaId)) {
      return ResponseEntity.notFound().build();
    }

    // Check if still processing
    if (mediaService.isMediaProcessing(mediaId)) {
      String statusUrl = String.format("%s/v1/media/%s/status",
          request.getServerName(), mediaId);

      HttpHeaders headers = new HttpHeaders();
      headers.add("Retry-After", "60");
      headers.add("Location", statusUrl);

      return ResponseEntity.accepted()
          .headers(headers)
          .body(MediaResponse.builder()
              .message("Media processing in progress.")
              .build());
    }

    return mediaService.getDownloadUrl(mediaId)
        .<ResponseEntity<?>>map(url -> ResponseEntity
            .status(HttpStatus.FOUND)
            .location(URI.create(url))
            .build())
        .orElse(ResponseEntity.notFound().build());
  }

  @PutMapping("/{mediaId}/resize")
  public ResponseEntity<?> resizeMedia(
      @PathVariable String mediaId,
      @Valid @RequestBody(required = false) ResizeRequest resizeRequest,
      @RequestParam(value = "width", required = false) Integer widthParam) {

    log.info("Resize request: mediaId={}", mediaId);

    // Get width from body or query param
    Integer width = (resizeRequest != null && resizeRequest.getWidth() != null)
        ? resizeRequest.getWidth()
        : widthParam;

    if (width == null || width < 100 || width > 1024) {
      return ResponseEntity.badRequest()
          .body(ErrorResponse.builder()
              .message("Width must be between 100 and 1024 pixels")
              .status(400)
              .build());
    }

    return mediaService.resizeMedia(mediaId, width)
        .<ResponseEntity<?>>map(response -> ResponseEntity.accepted().body(response))
        .orElse(ResponseEntity.notFound().build());
  }

  @DeleteMapping("/{mediaId}")
  public ResponseEntity<?> deleteMedia(@PathVariable String mediaId) {
    log.info("Delete request: mediaId={}", mediaId);

    return mediaService.deleteMedia(mediaId)
        .<ResponseEntity<?>>map(response -> ResponseEntity.accepted().body(response))
        .orElse(ResponseEntity.notFound().build());
  }
}
