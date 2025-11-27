package com.mediaservice.controller;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.dto.ErrorResponse;
import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.ResizeRequest;
import com.mediaservice.mapper.MediaMapper;
import com.mediaservice.service.MediaService;
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

  @PostMapping("/upload")
  public ResponseEntity<?> uploadMedia(
      @RequestParam("file") MultipartFile file,
      @RequestParam(value = "width", required = false) Integer width) {

    log.info("Upload request received: fileName={}, size={}",
        file.getOriginalFilename(), file.getSize());

    // Validate file size
    long maxFileSize = mediaProperties.getMaxFileSize();
    if (file.getSize() > maxFileSize) {
      long maxSizeMb = maxFileSize / (1024 * 1024);
      return ResponseEntity.badRequest()
          .body(ErrorResponse.builder()
              .message("Failed to upload media. Check the file size. Max size is " + maxSizeMb + " MB.")
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
        .<ResponseEntity<?>>map(media -> ResponseEntity.ok(mediaMapper.toResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }

  @GetMapping("/{mediaId}/status")
  public ResponseEntity<?> getMediaStatus(@PathVariable String mediaId) {
    log.info("Status request: mediaId={}", mediaId);
    return mediaService.getMediaStatus(mediaId)
        .<ResponseEntity<?>>map(status -> ResponseEntity.ok(mediaMapper.toStatusResponse(status)))
        .orElse(ResponseEntity.notFound().build());
  }

  @GetMapping("/{mediaId}/download")
  public ResponseEntity<?> downloadMedia(@PathVariable String mediaId, HttpServletRequest request) {

    log.info("Download request: mediaId={}", mediaId);

    if (!mediaService.mediaExists(mediaId)) {
      return ResponseEntity.notFound().build();
    }

    // Check if still processing
    if (mediaService.isMediaProcessing(mediaId)) {
      HttpHeaders headers = new HttpHeaders();
      headers.add("Retry-After", "60");
      headers.add("Location", "{}://{}:{}/v1/media/{}/status"
          .formatted(request.getScheme(), request.getServerName(), request.getServerPort(), mediaId));
      return ResponseEntity.accepted()
          .headers(headers)
          .body(mediaMapper.toMessageResponse("Media processing in progress."));
    }

    return mediaService.getDownloadUrl(mediaId)
        .<ResponseEntity<?>>map(url -> ResponseEntity
            .status(HttpStatus.FOUND)
            .location(URI.create(url))
            .build())
        .orElse(ResponseEntity.notFound().build());
  }

  @PutMapping("/{mediaId}/resize")
  public ResponseEntity<?> resizeMedia(@PathVariable String mediaId, @Valid @RequestBody ResizeRequest resizeRequest) {
    log.info("Resize request: mediaId={}", mediaId);
    return mediaService.resizeMedia(mediaId, resizeRequest.getWidth())
        .<ResponseEntity<?>>map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }

  @DeleteMapping("/{mediaId}")
  public ResponseEntity<?> deleteMedia(@PathVariable String mediaId) {
    log.info("Delete request: mediaId={}", mediaId);
    return mediaService.deleteMedia(mediaId)
        .<ResponseEntity<?>>map(media -> ResponseEntity.accepted().body(mediaMapper.toIdResponse(media)))
        .orElse(ResponseEntity.notFound().build());
  }
}
