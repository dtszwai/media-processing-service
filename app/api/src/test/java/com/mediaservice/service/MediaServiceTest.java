package com.mediaservice.service;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.dto.InitUploadRequest;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanBuilder;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.mock.web.MockMultipartFile;

import java.io.IOException;
import java.time.Instant;
import java.util.List;
import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.argThat;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class MediaServiceTest {

  @Mock
  private DynamoDbService dynamoDbService;
  @Mock
  private S3Service s3Service;
  @Mock
  private SnsService snsService;
  @Mock
  private ImageValidationService imageValidationService;
  @Mock
  private CacheInvalidationService cacheInvalidationService;
  @Mock
  private Tracer tracer;
  @Mock
  private Meter meter;
  @Mock
  private SpanBuilder spanBuilder;
  @Mock
  private Span span;
  @Mock
  private Scope scope;
  @Mock
  private LongCounter counter;

  private MediaProperties mediaProperties;
  private MediaService mediaService;

  @BeforeEach
  void setUp() {
    mediaProperties = new MediaProperties();
    mediaProperties.setMaxFileSize(100 * 1024 * 1024);
    var width = new MediaProperties.Width();
    width.setDefault(500);
    width.setMin(100);
    width.setMax(1024);
    mediaProperties.setWidth(width);
    var upload = new MediaProperties.Upload();
    upload.setPresignedUrlExpirationSeconds(3600);
    upload.setMaxPresignedUploadSize(5L * 1024 * 1024 * 1024);
    mediaProperties.setUpload(upload);

    lenient().when(tracer.spanBuilder(anyString())).thenReturn(spanBuilder);
    lenient().when(spanBuilder.setSpanKind(any())).thenReturn(spanBuilder);
    lenient().when(spanBuilder.startSpan()).thenReturn(span);
    lenient().when(span.makeCurrent()).thenReturn(scope);
    lenient().when(meter.counterBuilder(anyString()))
        .thenReturn(mock(io.opentelemetry.api.metrics.LongCounterBuilder.class));
    lenient().when(meter.counterBuilder(anyString()).setDescription(anyString()))
        .thenReturn(mock(io.opentelemetry.api.metrics.LongCounterBuilder.class));
    lenient().when(meter.counterBuilder(anyString()).setDescription(anyString()).build()).thenReturn(counter);

    mediaService = new MediaService(dynamoDbService, s3Service, snsService, mediaProperties, imageValidationService, cacheInvalidationService, tracer, meter);
  }

  @Nested
  @DisplayName("uploadMedia")
  class UploadMedia {
    @Test
    @DisplayName("should upload valid image and return media ID")
    void shouldUploadValidImage() throws IOException {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", "test-content".getBytes());
      var response = mediaService.uploadMedia(file, 500, "jpeg");
      assertThat(response.getMediaId()).isNotBlank();
      verify(s3Service).uploadMedia(anyString(), eq("test.jpg"), eq(file));
      verify(dynamoDbService).createMedia(any(Media.class));
      verify(snsService).publishProcessMediaEvent(anyString(), eq(500), eq("jpeg"));
    }

    @Test
    @DisplayName("should reject non-image content type")
    void shouldRejectNonImageContentType() {
      var file = new MockMultipartFile("file", "test.pdf", "application/pdf", "test".getBytes());
      assertThatThrownBy(() -> mediaService.uploadMedia(file, null, null))
          .isInstanceOf(IllegalArgumentException.class)
          .hasMessage("Invalid file type. Only images are supported.");
    }

    @Test
    @DisplayName("should reject null content type")
    void shouldRejectNullContentType() {
      var file = new MockMultipartFile("file", "test.jpg", null, "test".getBytes());
      assertThatThrownBy(() -> mediaService.uploadMedia(file, null, null))
          .isInstanceOf(IllegalArgumentException.class)
          .hasMessage("Invalid file type. Only images are supported.");
    }

    @Test
    @DisplayName("should use default filename when original is empty")
    void shouldUseDefaultFilenameWhenEmpty() throws IOException {
      var file = new MockMultipartFile("file", "", "image/jpeg", "test".getBytes());
      mediaService.uploadMedia(file, null, null);
      verify(s3Service).uploadMedia(anyString(), eq("image.jpg"), eq(file));
    }

    @Test
    @DisplayName("should use default width when not specified")
    void shouldUseDefaultWidth() throws IOException {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", "test".getBytes());
      mediaService.uploadMedia(file, null, null);
      verify(snsService).publishProcessMediaEvent(anyString(), eq(500), eq("jpeg"));
    }
  }

  @Nested
  @DisplayName("getMediaStatus")
  class GetMediaStatus {
    @Test
    @DisplayName("should return status when media exists")
    void shouldReturnStatusWhenMediaExists() {
      var media = createMedia(MediaStatus.PROCESSING);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      var result = mediaService.getMediaStatus("media-123");
      assertThat(result).contains(MediaStatus.PROCESSING);
    }

    @Test
    @DisplayName("should return empty when media not found")
    void shouldReturnEmptyWhenNotFound() {
      when(dynamoDbService.getMedia("nonexistent")).thenReturn(Optional.empty());
      var result = mediaService.getMediaStatus("nonexistent");
      assertThat(result).isEmpty();
    }
  }

  @Nested
  @DisplayName("getDownloadUrl")
  class GetDownloadUrl {

    @Test
    @DisplayName("should return URL when media is complete")
    void shouldReturnUrlWhenComplete() {
      var media = createMedia(MediaStatus.COMPLETE);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(s3Service.getPresignedUrl("media-123", "test.jpg", OutputFormat.JPEG))
          .thenReturn("https://s3.example.com/signed-url");
      var result = mediaService.getDownloadUrl("media-123");
      assertThat(result).contains("https://s3.example.com/signed-url");
    }

    @Test
    @DisplayName("should return empty when media is not complete")
    void shouldReturnEmptyWhenNotComplete() {
      var media = createMedia(MediaStatus.PROCESSING);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      var result = mediaService.getDownloadUrl("media-123");
      assertThat(result).isEmpty();
      verify(s3Service, never()).getPresignedUrl(anyString(), anyString(), any());
    }
  }

  @Nested
  @DisplayName("resizeMedia")
  class ResizeMedia {

    @Test
    @DisplayName("should submit resize request when media is complete")
    void shouldSubmitResizeWhenComplete() {
      var media = createMedia(MediaStatus.COMPLETE);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(dynamoDbService.updateStatusConditionally("media-123", MediaStatus.PENDING, MediaStatus.COMPLETE))
          .thenReturn(true);
      var result = mediaService.resizeMedia("media-123", 800, "jpeg");
      assertThat(result).isPresent();
      verify(snsService).publishResizeMediaEvent("media-123", 800, "jpeg");
    }

    @Test
    @DisplayName("should return empty when status update fails")
    void shouldReturnEmptyWhenStatusUpdateFails() {
      var media = createMedia(MediaStatus.PROCESSING);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(dynamoDbService.updateStatusConditionally("media-123", MediaStatus.PENDING, MediaStatus.COMPLETE))
          .thenReturn(false);
      var result = mediaService.resizeMedia("media-123", 800, null);
      assertThat(result).isEmpty();
      verify(snsService, never()).publishResizeMediaEvent(anyString(), any(), any());
    }
  }

  @Nested
  @DisplayName("deleteMedia")
  class DeleteMedia {
    @Test
    @DisplayName("should submit delete request when media exists")
    void shouldSubmitDeleteWhenExists() {
      var media = createMedia(MediaStatus.COMPLETE);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      var result = mediaService.deleteMedia("media-123");
      assertThat(result).isPresent();
      verify(dynamoDbService).updateStatus("media-123", MediaStatus.DELETING);
      verify(snsService).publishDeleteMediaEvent("media-123");
    }

    @Test
    @DisplayName("should return empty when media not found")
    void shouldReturnEmptyWhenNotFound() {
      when(dynamoDbService.getMedia("nonexistent")).thenReturn(Optional.empty());
      var result = mediaService.deleteMedia("nonexistent");
      assertThat(result).isEmpty();
      verify(snsService, never()).publishDeleteMediaEvent(anyString());
    }
  }

  @Nested
  @DisplayName("getMediaPaginated")
  class GetMediaPaginated {
    @Test
    @DisplayName("should return paginated media from database")
    void shouldReturnPaginatedMedia() {
      var mediaList = List.of(createMedia(MediaStatus.COMPLETE), createMedia(MediaStatus.PROCESSING));
      var pagedResult = new DynamoDbService.PagedResult(mediaList, "nextCursor", true);
      when(dynamoDbService.getMediaPaginated(null, null)).thenReturn(pagedResult);

      var result = mediaService.getMediaPaginated(null, null);

      assertThat(result.items()).hasSize(2);
      assertThat(result.nextCursor()).isEqualTo("nextCursor");
      assertThat(result.hasMore()).isTrue();
    }

    @Test
    @DisplayName("should pass cursor and limit to dynamodb service")
    void shouldPassCursorAndLimit() {
      var pagedResult = new DynamoDbService.PagedResult(List.of(), null, false);
      when(dynamoDbService.getMediaPaginated("someCursor", 10)).thenReturn(pagedResult);

      mediaService.getMediaPaginated("someCursor", 10);

      verify(dynamoDbService).getMediaPaginated("someCursor", 10);
    }
  }

  @Nested
  @DisplayName("initPresignedUpload")
  class InitPresignedUpload {
    @Test
    @DisplayName("should initialize presigned upload and return response")
    void shouldInitializePresignedUpload() {
      var request = InitUploadRequest.builder()
          .fileName("large-image.jpg")
          .fileSize(50 * 1024 * 1024L)
          .contentType("image/jpeg")
          .width(800)
          .build();

      when(s3Service.generatePresignedUploadUrl(anyString(), eq("large-image.jpg"), eq("image/jpeg"), any()))
          .thenReturn("https://s3.example.com/presigned-upload-url");

      var response = mediaService.initPresignedUpload(request);

      assertThat(response.getMediaId()).isNotBlank();
      assertThat(response.getUploadUrl()).isEqualTo("https://s3.example.com/presigned-upload-url");
      assertThat(response.getExpiresIn()).isEqualTo(3600);
      assertThat(response.getMethod()).isEqualTo("PUT");
      assertThat(response.getHeaders()).containsEntry("Content-Type", "image/jpeg");

      verify(dynamoDbService).createMedia(argThat(media -> media.getStatus() == MediaStatus.PENDING_UPLOAD &&
          media.getName().equals("large-image.jpg") &&
          media.getMimetype().equals("image/jpeg") &&
          media.getWidth() == 800), any());
    }

    @Test
    @DisplayName("should use default width when not specified")
    void shouldUseDefaultWidth() {
      var request = InitUploadRequest.builder()
          .fileName("image.jpg")
          .fileSize(1024L)
          .contentType("image/jpeg")
          .build();

      when(s3Service.generatePresignedUploadUrl(anyString(), anyString(), anyString(), any()))
          .thenReturn("https://s3.example.com/url");

      mediaService.initPresignedUpload(request);

      verify(dynamoDbService).createMedia(argThat(media -> media.getWidth() == 500), any());
    }
  }

  @Nested
  @DisplayName("completePresignedUpload")
  class CompletePresignedUpload {

    @Test
    @DisplayName("should complete upload when file exists in S3")
    void shouldCompleteUploadWhenFileExists() {
      var media = createMedia(MediaStatus.PENDING_UPLOAD);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(s3Service.objectExists("media-123", "test.jpg")).thenReturn(true);
      when(dynamoDbService.updateStatusConditionally("media-123", MediaStatus.PENDING, MediaStatus.PENDING_UPLOAD, true))
          .thenReturn(true);

      var result = mediaService.completePresignedUpload("media-123");

      assertThat(result).isPresent();
      verify(snsService).publishProcessMediaEvent("media-123", 500, "jpeg");
    }

    @Test
    @DisplayName("should return empty when media not found")
    void shouldReturnEmptyWhenNotFound() {
      when(dynamoDbService.getMedia("nonexistent")).thenReturn(Optional.empty());

      var result = mediaService.completePresignedUpload("nonexistent");

      assertThat(result).isEmpty();
      verify(s3Service, never()).objectExists(anyString(), anyString());
      verify(snsService, never()).publishProcessMediaEvent(anyString(), any(), any());
    }

    @Test
    @DisplayName("should return empty when status is not PENDING_UPLOAD")
    void shouldReturnEmptyWhenWrongStatus() {
      var media = createMedia(MediaStatus.COMPLETE);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));

      var result = mediaService.completePresignedUpload("media-123");

      assertThat(result).isEmpty();
      verify(s3Service, never()).objectExists(anyString(), anyString());
    }

    @Test
    @DisplayName("should return empty when file not found in S3")
    void shouldReturnEmptyWhenFileNotInS3() {
      var media = createMedia(MediaStatus.PENDING_UPLOAD);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(s3Service.objectExists("media-123", "test.jpg")).thenReturn(false);

      var result = mediaService.completePresignedUpload("media-123");

      assertThat(result).isEmpty();
      verify(dynamoDbService, never()).updateStatusConditionally(anyString(), any(), any());
      verify(snsService, never()).publishProcessMediaEvent(anyString(), any(), any());
    }

    @Test
    @DisplayName("should return empty when status update fails")
    void shouldReturnEmptyWhenStatusUpdateFails() {
      var media = createMedia(MediaStatus.PENDING_UPLOAD);
      when(dynamoDbService.getMedia("media-123")).thenReturn(Optional.of(media));
      when(s3Service.objectExists("media-123", "test.jpg")).thenReturn(true);
      when(dynamoDbService.updateStatusConditionally("media-123", MediaStatus.PENDING, MediaStatus.PENDING_UPLOAD, true))
          .thenReturn(false);

      var result = mediaService.completePresignedUpload("media-123");

      assertThat(result).isEmpty();
      verify(snsService, never()).publishProcessMediaEvent(anyString(), any(), any());
    }
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
        .build();
  }
}
