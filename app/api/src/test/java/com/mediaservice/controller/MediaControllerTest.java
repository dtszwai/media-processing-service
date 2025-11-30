package com.mediaservice.controller;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.config.RateLimitingConfig;
import com.mediaservice.dto.InitUploadResponse;
import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.StatusResponse;
import com.mediaservice.filter.RateLimitingFilter;
import com.mediaservice.filter.RequestIdFilter;
import com.mediaservice.filter.SecurityHeadersFilter;
import com.mediaservice.mapper.MediaMapper;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.service.DynamoDbService;
import com.mediaservice.service.MediaService;

import java.util.Map;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.context.annotation.ComponentScan;
import org.springframework.context.annotation.FilterType;
import org.springframework.http.MediaType;
import org.springframework.mock.web.MockMultipartFile;
import org.springframework.test.web.servlet.MockMvc;

import java.time.Instant;
import java.util.List;
import java.util.Optional;

import static org.hamcrest.Matchers.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@WebMvcTest(value = MediaController.class, excludeFilters = {
    @ComponentScan.Filter(type = FilterType.ASSIGNABLE_TYPE, classes = {
        RateLimitingFilter.class,
        SecurityHeadersFilter.class,
        RequestIdFilter.class
    })
})
class MediaControllerTest {

  @Autowired
  private MockMvc mockMvc;

  @MockBean
  private MediaService mediaService;

  @MockBean
  private MediaMapper mediaMapper;

  @MockBean
  private MediaProperties mediaProperties;

  @MockBean
  private RateLimitingConfig rateLimitingConfig;

  @Nested
  @DisplayName("GET /v1/media/health")
  class HealthCheck {

    @Test
    @DisplayName("should return OK")
    void shouldReturnOk() throws Exception {
      mockMvc.perform(get("/v1/media/health"))
          .andExpect(status().isOk())
          .andExpect(content().string("OK"));
    }
  }

  @Nested
  @DisplayName("POST /v1/media/upload")
  class Upload {

    @Test
    @DisplayName("should upload valid image file")
    void shouldUploadValidFile() throws Exception {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", "test-content".getBytes());
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaProperties.getMaxFileSize()).thenReturn(100L * 1024 * 1024);
      when(mediaService.uploadMedia(any(), any(), any())).thenReturn(response);

      mockMvc.perform(multipart("/v1/media/upload").file(file))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should reject file exceeding size limit")
    void shouldRejectLargeFile() throws Exception {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", new byte[101 * 1024 * 1024]);

      when(mediaProperties.getMaxFileSize()).thenReturn(100L * 1024 * 1024);

      mockMvc.perform(multipart("/v1/media/upload").file(file))
          .andExpect(status().isBadRequest())
          .andExpect(jsonPath("$.message", containsString("Max size is 100 MB")));
    }

    @Test
    @DisplayName("should reject empty file")
    void shouldRejectEmptyFile() throws Exception {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", new byte[0]);

      when(mediaProperties.getMaxFileSize()).thenReturn(100L * 1024 * 1024);

      mockMvc.perform(multipart("/v1/media/upload").file(file))
          .andExpect(status().isBadRequest())
          .andExpect(jsonPath("$.message").value("Malformed multipart form data."));
    }
  }

  @Nested
  @DisplayName("GET /v1/media/{mediaId}")
  class GetMedia {

    @Test
    @DisplayName("should return media when found")
    void shouldReturnMediaWhenFound() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(mediaMapper.toResponse(media)).thenReturn(response);

      mockMvc.perform(get("/v1/media/{mediaId}", "media-123"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.getMedia("nonexistent")).thenReturn(Optional.empty());

      mockMvc.perform(get("/v1/media/{mediaId}", "nonexistent"))
          .andExpect(status().isNotFound());
    }
  }

  @Nested
  @DisplayName("GET /v1/media/{mediaId}/status")
  class GetStatus {

    @Test
    @DisplayName("should return status when media exists")
    void shouldReturnStatus() throws Exception {
      var statusResponse = StatusResponse.builder().status(MediaStatus.PROCESSING).build();

      when(mediaService.getMediaStatus("media-123")).thenReturn(Optional.of(MediaStatus.PROCESSING));
      when(mediaMapper.toStatusResponse(MediaStatus.PROCESSING)).thenReturn(statusResponse);

      mockMvc.perform(get("/v1/media/{mediaId}/status", "media-123"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.status").value("PROCESSING"));
    }
  }

  @Nested
  @DisplayName("GET /v1/media/{mediaId}/download")
  class Download {

    @Test
    @DisplayName("should redirect when media is complete")
    void shouldRedirectWhenComplete() throws Exception {
      when(mediaService.mediaExists("media-123")).thenReturn(true);
      when(mediaService.isMediaProcessing("media-123")).thenReturn(false);
      when(mediaService.getDownloadUrl("media-123")).thenReturn(Optional.of("https://s3.example.com/file"));

      mockMvc.perform(get("/v1/media/{mediaId}/download", "media-123"))
          .andExpect(status().isFound())
          .andExpect(header().string("Location", "https://s3.example.com/file"));
    }

    @Test
    @DisplayName("should return 202 when still processing")
    void shouldReturn202WhenProcessing() throws Exception {
      var messageResponse = MediaResponse.builder()
          .message("Media processing in progress.").build();

      when(mediaService.mediaExists("media-123")).thenReturn(true);
      when(mediaService.isMediaProcessing("media-123")).thenReturn(true);
      when(mediaMapper.toMessageResponse(any())).thenReturn(messageResponse);

      mockMvc.perform(get("/v1/media/{mediaId}/download", "media-123"))
          .andExpect(status().isAccepted())
          .andExpect(header().exists("Retry-After"))
          .andExpect(jsonPath("$.message").value("Media processing in progress."));
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.mediaExists("nonexistent")).thenReturn(false);

      mockMvc.perform(get("/v1/media/{mediaId}/download", "nonexistent"))
          .andExpect(status().isNotFound());
    }
  }

  @Nested
  @DisplayName("PUT /v1/media/{mediaId}/resize")
  class Resize {

    @Test
    @DisplayName("should accept resize request")
    void shouldAcceptResize() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaService.mediaExists("media-123")).thenReturn(true);
      when(mediaService.resizeMedia(eq("media-123"), eq(800), any())).thenReturn(Optional.of(media));
      when(mediaMapper.toIdResponse(media)).thenReturn(response);

      mockMvc.perform(put("/v1/media/{mediaId}/resize", "media-123")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{\"width\": 800}"))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 409 when resize not allowed")
    void shouldReturn409WhenNotAllowed() throws Exception {
      when(mediaService.mediaExists("media-123")).thenReturn(true);
      when(mediaService.resizeMedia(eq("media-123"), eq(800), any())).thenReturn(Optional.empty());

      mockMvc.perform(put("/v1/media/{mediaId}/resize", "media-123")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{\"width\": 800}"))
          .andExpect(status().isConflict());
    }
  }

  @Nested
  @DisplayName("DELETE /v1/media/{mediaId}")
  class Delete {

    @Test
    @DisplayName("should accept delete request")
    void shouldAcceptDelete() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaService.deleteMedia("media-123")).thenReturn(Optional.of(media));
      when(mediaMapper.toIdResponse(media)).thenReturn(response);

      mockMvc.perform(delete("/v1/media/{mediaId}", "media-123"))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.deleteMedia("nonexistent")).thenReturn(Optional.empty());

      mockMvc.perform(delete("/v1/media/{mediaId}", "nonexistent"))
          .andExpect(status().isNotFound());
    }
  }

  @Nested
  @DisplayName("GET /v1/media")
  class GetAll {

    @Test
    @DisplayName("should return paginated media")
    void shouldReturnPaginatedMedia() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();
      var pagedResult = new DynamoDbService.PagedResult(List.of(media), null, false);

      when(mediaService.getMediaPaginated(null, null)).thenReturn(pagedResult);
      when(mediaMapper.toResponse(media)).thenReturn(response);

      mockMvc.perform(get("/v1/media"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.items", hasSize(1)))
          .andExpect(jsonPath("$.items[0].mediaId").value("media-123"))
          .andExpect(jsonPath("$.hasMore").value(false));
    }

    @Test
    @DisplayName("should return next cursor when has more")
    void shouldReturnNextCursorWhenHasMore() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();
      var pagedResult = new DynamoDbService.PagedResult(List.of(media), "nextCursor123", true);

      when(mediaService.getMediaPaginated(null, null)).thenReturn(pagedResult);
      when(mediaMapper.toResponse(media)).thenReturn(response);

      mockMvc.perform(get("/v1/media"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.items", hasSize(1)))
          .andExpect(jsonPath("$.nextCursor").value("nextCursor123"))
          .andExpect(jsonPath("$.hasMore").value(true));
    }

    @Test
    @DisplayName("should pass cursor and limit parameters")
    void shouldPassCursorAndLimit() throws Exception {
      var pagedResult = new DynamoDbService.PagedResult(List.of(), null, false);

      when(mediaService.getMediaPaginated("someCursor", 10)).thenReturn(pagedResult);

      mockMvc.perform(get("/v1/media")
          .param("cursor", "someCursor")
          .param("limit", "10"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.items", hasSize(0)));

      verify(mediaService).getMediaPaginated("someCursor", 10);
    }
  }

  @Nested
  @DisplayName("POST /v1/media/upload/init")
  class InitPresignedUpload {

    @Test
    @DisplayName("should initialize presigned upload")
    void shouldInitializePresignedUpload() throws Exception {
      var uploadConfig = new MediaProperties.Upload();
      uploadConfig.setMaxPresignedUploadSize(5L * 1024 * 1024 * 1024);

      var response = InitUploadResponse.builder()
          .mediaId("media-123")
          .uploadUrl("https://s3.example.com/presigned-url")
          .expiresIn(3600)
          .method("PUT")
          .headers(Map.of("Content-Type", "image/jpeg"))
          .build();

      when(mediaProperties.getUpload()).thenReturn(uploadConfig);
      when(mediaService.initPresignedUpload(any())).thenReturn(response);

      mockMvc.perform(post("/v1/media/upload/init")
          .contentType(MediaType.APPLICATION_JSON)
          .content("""
              {
                "fileName": "large-image.jpg",
                "fileSize": 52428800,
                "contentType": "image/jpeg",
                "width": 800
              }
              """))
          .andExpect(status().isCreated())
          .andExpect(jsonPath("$.mediaId").value("media-123"))
          .andExpect(jsonPath("$.uploadUrl").value("https://s3.example.com/presigned-url"))
          .andExpect(jsonPath("$.expiresIn").value(3600))
          .andExpect(jsonPath("$.method").value("PUT"));
    }

    @Test
    @DisplayName("should reject file exceeding size limit")
    void shouldRejectLargeFile() throws Exception {
      var uploadConfig = new MediaProperties.Upload();
      uploadConfig.setMaxPresignedUploadSize(5L * 1024 * 1024 * 1024);

      when(mediaProperties.getUpload()).thenReturn(uploadConfig);

      mockMvc.perform(post("/v1/media/upload/init")
          .contentType(MediaType.APPLICATION_JSON)
          .content("""
              {
                "fileName": "huge-file.jpg",
                "fileSize": 6000000000000,
                "contentType": "image/jpeg"
              }
              """))
          .andExpect(status().isBadRequest())
          .andExpect(jsonPath("$.message", containsString("exceeds maximum")));
    }

    @Test
    @DisplayName("should reject non-image content type")
    void shouldRejectNonImageContentType() throws Exception {
      var uploadConfig = new MediaProperties.Upload();
      uploadConfig.setMaxPresignedUploadSize(5L * 1024 * 1024 * 1024);

      when(mediaProperties.getUpload()).thenReturn(uploadConfig);

      mockMvc.perform(post("/v1/media/upload/init")
          .contentType(MediaType.APPLICATION_JSON)
          .content("""
              {
                "fileName": "document.pdf",
                "fileSize": 1024,
                "contentType": "application/pdf"
              }
              """))
          .andExpect(status().isBadRequest())
          .andExpect(jsonPath("$.message", containsString("Only images are supported")));
    }

    @Test
    @DisplayName("should reject missing required fields")
    void shouldRejectMissingFields() throws Exception {
      var uploadConfig = new MediaProperties.Upload();
      uploadConfig.setMaxPresignedUploadSize(5L * 1024 * 1024 * 1024);

      when(mediaProperties.getUpload()).thenReturn(uploadConfig);

      mockMvc.perform(post("/v1/media/upload/init")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{}"))
          .andExpect(status().isBadRequest());
    }
  }

  @Nested
  @DisplayName("POST /v1/media/{mediaId}/upload/complete")
  class CompletePresignedUpload {

    @Test
    @DisplayName("should complete presigned upload")
    void shouldCompletePresignedUpload() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaService.completePresignedUpload("media-123")).thenReturn(Optional.of(media));
      when(mediaMapper.toIdResponse(media)).thenReturn(response);

      mockMvc.perform(post("/v1/media/{mediaId}/upload/complete", "media-123"))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 400 when upload not found or not ready")
    void shouldReturn400WhenNotFound() throws Exception {
      when(mediaService.completePresignedUpload("nonexistent")).thenReturn(Optional.empty());

      mockMvc.perform(post("/v1/media/{mediaId}/upload/complete", "nonexistent"))
          .andExpect(status().isBadRequest())
          .andExpect(jsonPath("$.message", containsString("not in PENDING_UPLOAD status")));
    }
  }

  private Media createMedia() {
    return Media.builder()
        .mediaId("media-123")
        .name("test.jpg")
        .size(1024L)
        .mimetype("image/jpeg")
        .status(MediaStatus.COMPLETE)
        .width(500)
        .createdAt(Instant.now())
        .updatedAt(Instant.now())
        .build();
  }
}
