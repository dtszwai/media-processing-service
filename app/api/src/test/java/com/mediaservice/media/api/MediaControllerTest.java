package com.mediaservice.media.api;

import com.mediaservice.shared.cache.config.RateLimitingConfig;
import com.mediaservice.media.api.dto.InitUploadResponse;
import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.media.api.dto.StatusResponse;
import com.mediaservice.media.application.DownloadResult;
import com.mediaservice.media.application.MediaOperationResult;
import com.mediaservice.media.application.PreviewResult;
import com.mediaservice.shared.http.error.MediaGoneException;
import com.mediaservice.shared.http.filter.RateLimitingFilter;
import com.mediaservice.shared.http.filter.RequestIdFilter;
import com.mediaservice.shared.http.filter.SecurityHeadersFilter;
import com.mediaservice.media.application.mapper.MediaMapper;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import com.mediaservice.media.application.MediaApplicationService;

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
import static org.mockito.ArgumentMatchers.anyBoolean;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.anyString;
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
  private MediaApplicationService mediaService;

  @MockBean
  private MediaMapper mediaMapper;

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

      doNothing().when(mediaService).validateUploadFile(anyLong(), anyBoolean());
      when(mediaService.uploadMedia(any(), any(), any())).thenReturn(response);

      mockMvc.perform(multipart("/v1/media/upload").file(file))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should reject file exceeding size limit")
    void shouldRejectLargeFile() throws Exception {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", new byte[101 * 1024 * 1024]);

      doThrow(new IllegalArgumentException("Failed to upload media. Check the file size. Max size is 100 MB."))
          .when(mediaService).validateUploadFile(anyLong(), anyBoolean());

      mockMvc.perform(multipart("/v1/media/upload").file(file))
          .andExpect(status().isBadRequest())
          .andExpect(jsonPath("$.message", containsString("Max size is 100 MB")));
    }

    @Test
    @DisplayName("should reject empty file")
    void shouldRejectEmptyFile() throws Exception {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", new byte[0]);

      doThrow(new IllegalArgumentException("Malformed multipart form data."))
          .when(mediaService).validateUploadFile(anyLong(), eq(true));

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

      when(mediaService.getActiveMedia("media-123")).thenReturn(Optional.of(media));
      when(mediaMapper.toResponse(media)).thenReturn(response);

      mockMvc.perform(get("/v1/media/{mediaId}", "media-123"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.getActiveMedia("nonexistent")).thenReturn(Optional.empty());

      mockMvc.perform(get("/v1/media/{mediaId}", "nonexistent"))
          .andExpect(status().isNotFound());
    }

    @Test
    @DisplayName("should return 410 when deleted")
    void shouldReturn410WhenDeleted() throws Exception {
      when(mediaService.getActiveMedia("media-123"))
          .thenThrow(new MediaGoneException("Media has been deleted", Instant.now()));

      mockMvc.perform(get("/v1/media/{mediaId}", "media-123"))
          .andExpect(status().isGone())
          .andExpect(jsonPath("$.message").value("Media has been deleted"));
    }
  }

  @Nested
  @DisplayName("GET /v1/media/{mediaId}/status")
  class GetStatus {

    @Test
    @DisplayName("should return status when media exists")
    void shouldReturnStatus() throws Exception {
      var statusResponse = StatusResponse.builder().status(MediaStatus.PROCESSING).build();

      when(mediaService.getActiveMediaStatus("media-123")).thenReturn(Optional.of(MediaStatus.PROCESSING));
      when(mediaMapper.toStatusResponse(MediaStatus.PROCESSING)).thenReturn(statusResponse);

      mockMvc.perform(get("/v1/media/{mediaId}/status", "media-123"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$.status").value("PROCESSING"));
    }

    @Test
    @DisplayName("should return 410 when deleted")
    void shouldReturn410WhenDeleted() throws Exception {
      when(mediaService.getActiveMediaStatus("media-123"))
          .thenThrow(new MediaGoneException("Media has been deleted"));

      mockMvc.perform(get("/v1/media/{mediaId}/status", "media-123"))
          .andExpect(status().isGone())
          .andExpect(jsonPath("$.message").value("Media has been deleted"));
    }
  }

  @Nested
  @DisplayName("GET /v1/media/{mediaId}/download")
  class Download {

    @Test
    @DisplayName("should redirect when media is complete")
    void shouldRedirectWhenComplete() throws Exception {
      var media = createMedia();
      when(mediaService.prepareDownload("media-123"))
          .thenReturn(new DownloadResult.Ready("https://s3.example.com/file", media));

      mockMvc.perform(get("/v1/media/{mediaId}/download", "media-123"))
          .andExpect(status().isFound())
          .andExpect(header().string("Location", "https://s3.example.com/file"));
    }

    @Test
    @DisplayName("should return 202 when still processing")
    void shouldReturn202WhenProcessing() throws Exception {
      var messageResponse = MediaResponse.builder()
          .message("Media processing in progress.").build();

      when(mediaService.prepareDownload("media-123"))
          .thenReturn(new DownloadResult.Processing("media-123"));
      when(mediaMapper.toMessageResponse(any())).thenReturn(messageResponse);

      mockMvc.perform(get("/v1/media/{mediaId}/download", "media-123"))
          .andExpect(status().isAccepted())
          .andExpect(header().exists("Retry-After"))
          .andExpect(jsonPath("$.message").value("Media processing in progress."));
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.prepareDownload("nonexistent"))
          .thenReturn(new DownloadResult.NotFound());

      mockMvc.perform(get("/v1/media/{mediaId}/download", "nonexistent"))
          .andExpect(status().isNotFound());
    }

    @Test
    @DisplayName("should return 410 when media is deleted")
    void shouldReturn410WhenDeleted() throws Exception {
      when(mediaService.prepareDownload("media-123"))
          .thenThrow(new MediaGoneException("Media has been deleted", Instant.now()));

      mockMvc.perform(get("/v1/media/{mediaId}/download", "media-123"))
          .andExpect(status().isGone())
          .andExpect(jsonPath("$.message").value("Media has been deleted"));
    }
  }

  @Nested
  @DisplayName("GET /v1/media/{mediaId}/preview")
  class Preview {

    @Test
    @DisplayName("should redirect when preview is ready")
    void shouldRedirectWhenReady() throws Exception {
      when(mediaService.preparePreview("media-123"))
          .thenReturn(new PreviewResult.Ready("https://cdn.example.com/preview"));

      mockMvc.perform(get("/v1/media/{mediaId}/preview", "media-123"))
          .andExpect(status().isFound())
          .andExpect(header().string("Location", "https://cdn.example.com/preview"));
    }

    @Test
    @DisplayName("should return 202 when still processing")
    void shouldReturn202WhenProcessing() throws Exception {
      when(mediaService.preparePreview("media-123"))
          .thenReturn(new PreviewResult.Processing("media-123"));

      mockMvc.perform(get("/v1/media/{mediaId}/preview", "media-123"))
          .andExpect(status().isAccepted());
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.preparePreview("nonexistent"))
          .thenReturn(new PreviewResult.NotFound());

      mockMvc.perform(get("/v1/media/{mediaId}/preview", "nonexistent"))
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

      when(mediaService.resizeMedia(eq("media-123"), eq(800), any()))
          .thenReturn(new MediaOperationResult.Success(media));
      when(mediaMapper.toIdResponse(media)).thenReturn(response);

      mockMvc.perform(put("/v1/media/{mediaId}/resize", "media-123")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{\"width\": 800}"))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 404 when media not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.resizeMedia(eq("media-123"), eq(800), any()))
          .thenReturn(new MediaOperationResult.NotFound("media-123"));

      mockMvc.perform(put("/v1/media/{mediaId}/resize", "media-123")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{\"width\": 800}"))
          .andExpect(status().isNotFound());
    }

    @Test
    @DisplayName("should return 409 when resize not allowed")
    void shouldReturn409WhenNotAllowed() throws Exception {
      when(mediaService.resizeMedia(eq("media-123"), eq(800), any()))
          .thenReturn(new MediaOperationResult.NotAllowed("media-123", "Not in COMPLETE status"));

      mockMvc.perform(put("/v1/media/{mediaId}/resize", "media-123")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{\"width\": 800}"))
          .andExpect(status().isConflict());
    }

    @Test
    @DisplayName("should return 410 when media is deleted")
    void shouldReturn410WhenDeleted() throws Exception {
      when(mediaService.resizeMedia(eq("media-123"), eq(800), any()))
          .thenReturn(new MediaOperationResult.Deleted("media-123", Instant.now()));

      mockMvc.perform(put("/v1/media/{mediaId}/resize", "media-123")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{\"width\": 800}"))
          .andExpect(status().isGone());
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
      var pagedResult = new MediaDynamoDbRepository.MediaPagedResult(List.of(media), null, false);

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
      var pagedResult = new MediaDynamoDbRepository.MediaPagedResult(List.of(media), "nextCursor123", true);

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
      var pagedResult = new MediaDynamoDbRepository.MediaPagedResult(List.of(), null, false);

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
      var response = InitUploadResponse.builder()
          .mediaId("media-123")
          .uploadUrl("https://s3.example.com/presigned-url")
          .expiresIn(3600)
          .method("PUT")
          .headers(Map.of("Content-Type", "image/jpeg"))
          .build();

      doNothing().when(mediaService).validatePresignedUploadRequest(anyLong(), anyString());
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
      doThrow(new IllegalArgumentException("File size exceeds maximum allowed size of 5 GB."))
          .when(mediaService).validatePresignedUploadRequest(anyLong(), anyString());

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
      doThrow(new IllegalArgumentException("Invalid content type. Only images are supported."))
          .when(mediaService).validatePresignedUploadRequest(anyLong(), anyString());

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
      mockMvc.perform(post("/v1/media/upload/init")
          .contentType(MediaType.APPLICATION_JSON)
          .content("{}"))
          .andExpect(status().isBadRequest());
    }
  }

  @Nested
  @DisplayName("POST /v1/media/{mediaId}/retry")
  class Retry {

    @Test
    @DisplayName("should accept retry request")
    void shouldAcceptRetry() throws Exception {
      var media = createMedia(MediaStatus.ERROR);
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaService.retryProcessing("media-123"))
          .thenReturn(new MediaOperationResult.Success(media));
      when(mediaMapper.toIdResponse(media)).thenReturn(response);

      mockMvc.perform(post("/v1/media/{mediaId}/retry", "media-123"))
          .andExpect(status().isAccepted())
          .andExpect(jsonPath("$.mediaId").value("media-123"));
    }

    @Test
    @DisplayName("should return 404 when media not found")
    void shouldReturn404WhenNotFound() throws Exception {
      when(mediaService.retryProcessing("nonexistent"))
          .thenReturn(new MediaOperationResult.NotFound("nonexistent"));

      mockMvc.perform(post("/v1/media/{mediaId}/retry", "nonexistent"))
          .andExpect(status().isNotFound());
    }

    @Test
    @DisplayName("should return 409 when retry not allowed")
    void shouldReturn409WhenNotAllowed() throws Exception {
      when(mediaService.retryProcessing("media-123"))
          .thenReturn(new MediaOperationResult.NotAllowed("media-123", "Not in ERROR status"));

      mockMvc.perform(post("/v1/media/{mediaId}/retry", "media-123"))
          .andExpect(status().isConflict());
    }

    @Test
    @DisplayName("should return 410 when media is deleted")
    void shouldReturn410WhenDeleted() throws Exception {
      when(mediaService.retryProcessing("media-123"))
          .thenReturn(new MediaOperationResult.Deleted("media-123", Instant.now()));

      mockMvc.perform(post("/v1/media/{mediaId}/retry", "media-123"))
          .andExpect(status().isGone());
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
    return createMedia(MediaStatus.COMPLETE);
  }

  private Media createMedia(MediaStatus status) {
    return Media.builder()
        .mediaId("media-123")
        .name("test.jpg")
        .size(1024L)
        .mimetype("image/jpeg")
        .status(status)
        .width(500)
        .outputFormat(OutputFormat.JPEG)
        .createdAt(Instant.now())
        .updatedAt(Instant.now())
        .deletedAt(status == MediaStatus.DELETED ? Instant.now() : null)
        .build();
  }
}
