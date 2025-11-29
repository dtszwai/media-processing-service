package com.mediaservice.controller;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.StatusResponse;
import com.mediaservice.mapper.MediaMapper;
import com.mediaservice.model.Media;
import com.mediaservice.model.MediaStatus;
import com.mediaservice.service.MediaService;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
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

@WebMvcTest(MediaController.class)
class MediaControllerTest {

  @Autowired
  private MockMvc mockMvc;

  @MockBean
  private MediaService mediaService;

  @MockBean
  private MediaMapper mediaMapper;

  @MockBean
  private MediaProperties mediaProperties;

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
      when(mediaService.uploadMedia(any(), any())).thenReturn(response);

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
      when(mediaService.resizeMedia(eq("media-123"), eq(800))).thenReturn(Optional.of(media));
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
      when(mediaService.resizeMedia(eq("media-123"), eq(800))).thenReturn(Optional.empty());

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
    @DisplayName("should return all media")
    void shouldReturnAllMedia() throws Exception {
      var media = createMedia();
      var response = MediaResponse.builder().mediaId("media-123").build();

      when(mediaService.getAllMedia()).thenReturn(List.of(media));
      when(mediaMapper.toResponse(media)).thenReturn(response);

      mockMvc.perform(get("/v1/media"))
          .andExpect(status().isOk())
          .andExpect(jsonPath("$", hasSize(1)))
          .andExpect(jsonPath("$[0].mediaId").value("media-123"));
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
