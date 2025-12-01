package com.mediaservice.integration;

import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.core.io.ByteArrayResource;
import org.springframework.http.*;
import org.springframework.util.LinkedMultiValueMap;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.io.IOException;

import static org.assertj.core.api.Assertions.assertThat;

@Tag("integration")
class MediaApiIntegrationTest extends BaseIntegrationTest {

  @LocalServerPort
  private int port;

  @Autowired
  private TestRestTemplate restTemplate;

  @Autowired
  private MediaDynamoDbRepository dynamoDbService;

  private String baseUrl() {
    return "http://localhost:" + port + "/v1/media";
  }

  @Nested
  @DisplayName("Health Check")
  class HealthCheck {

    @Test
    @DisplayName("should return OK")
    void shouldReturnOk() {
      var response = restTemplate.getForEntity(baseUrl() + "/health", String.class);
      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
      assertThat(response.getBody()).isEqualTo("OK");
    }
  }

  @Nested
  @DisplayName("Upload Media")
  class UploadMedia {

    @Test
    @DisplayName("should upload image and return media ID")
    void shouldUploadImage() throws IOException {
      var headers = new HttpHeaders();
      headers.setContentType(MediaType.MULTIPART_FORM_DATA);

      byte[] imageBytes = createTestImage("jpg");
      var body = new LinkedMultiValueMap<String, Object>();
      body.add("file", new ByteArrayResource(imageBytes) {
        @Override
        public String getFilename() {
          return "test.jpg";
        }
      });

      var response = restTemplate.postForEntity(
          baseUrl() + "/upload",
          new HttpEntity<>(body, headers),
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
      assertThat(response.getBody()).contains("mediaId");
    }

    @Test
    @DisplayName("should reject empty file")
    void shouldRejectEmptyFile() {
      var headers = new HttpHeaders();
      headers.setContentType(MediaType.MULTIPART_FORM_DATA);

      var body = new LinkedMultiValueMap<String, Object>();
      body.add("file", new ByteArrayResource(new byte[0]) {
        @Override
        public String getFilename() {
          return "empty.jpg";
        }
      });

      var response = restTemplate.postForEntity(
          baseUrl() + "/upload",
          new HttpEntity<>(body, headers),
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }
  }

  @Nested
  @DisplayName("Get Media")
  class GetMedia {

    @Test
    @DisplayName("should return media when exists")
    void shouldReturnMedia() {
      createAndSaveMedia("media-123", MediaStatus.COMPLETE);
      var response = restTemplate.getForEntity(baseUrl() + "/media-123", String.class);
      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
      assertThat(response.getBody()).contains("media-123");
    }

    @Test
    @DisplayName("should return 404 when not found")
    void shouldReturn404WhenNotFound() {
      var response = restTemplate.getForEntity(baseUrl() + "/nonexistent", String.class);
      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }
  }

  @Nested
  @DisplayName("Get Media Status")
  class GetMediaStatus {

    @Test
    @DisplayName("should return current status")
    void shouldReturnStatus() {
      createAndSaveMedia("media-123", MediaStatus.PROCESSING);
      var response = restTemplate.getForEntity(baseUrl() + "/media-123/status", String.class);
      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
      assertThat(response.getBody()).contains("PROCESSING");
    }
  }

  @Nested
  @DisplayName("Download Media")
  class DownloadMedia {

    @Test
    @DisplayName("should return 202 when processing")
    void shouldReturn202WhenProcessing() {
      createAndSaveMedia("media-123", MediaStatus.PROCESSING);

      var response = restTemplate.getForEntity(
          baseUrl() + "/media-123/download", String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
      assertThat(response.getHeaders().get("Retry-After")).isNotNull();
    }

    @Test
    @DisplayName("should return presigned URL when complete")
    void shouldReturnPresignedUrlWhenComplete() {
      createAndSaveMedia("media-123", MediaStatus.COMPLETE);
      // Upload a file so presigned URL can be generated
      // File name should match the output format extension (test.jpeg for JPEG
      // format)
      s3Client.putObject(
          b -> b.bucket("media-bucket").key("resized/media-123/test.jpeg").contentType("image/jpeg"),
          software.amazon.awssdk.core.sync.RequestBody.fromBytes("test".getBytes()));
      // TestRestTemplate follows redirects, so we get the final response
      // The redirect to S3 presigned URL should work
      var response = restTemplate.getForEntity(baseUrl() + "/media-123/download", String.class);
      // The redirect should result in a 200 from S3 with the content
      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    }
  }

  @Nested
  @DisplayName("Delete Media")
  class DeleteMedia {
    @Test
    @DisplayName("should accept delete request")
    void shouldAcceptDelete() {
      createAndSaveMedia("media-123", MediaStatus.COMPLETE);
      var response = restTemplate.exchange(
          baseUrl() + "/media-123",
          HttpMethod.DELETE,
          null,
          String.class);
      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    }

    @Test
    @DisplayName("should return 404 for non-existent media")
    void shouldReturn404ForNonExistent() {
      var response = restTemplate.exchange(
          baseUrl() + "/nonexistent",
          HttpMethod.DELETE,
          null,
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    }
  }

  @Nested
  @DisplayName("Resize Media")
  class ResizeMedia {

    @Test
    @DisplayName("should accept resize request when complete")
    void shouldAcceptResize() {
      createAndSaveMedia("media-123", MediaStatus.COMPLETE);

      var headers = new HttpHeaders();
      headers.setContentType(MediaType.APPLICATION_JSON);

      var response = restTemplate.exchange(
          baseUrl() + "/media-123/resize",
          HttpMethod.PUT,
          new HttpEntity<>("{\"width\": 800}", headers),
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    }

    @Test
    @DisplayName("should return 409 when not in COMPLETE status")
    void shouldReturn409WhenNotComplete() {
      createAndSaveMedia("media-123", MediaStatus.PROCESSING);

      var headers = new HttpHeaders();
      headers.setContentType(MediaType.APPLICATION_JSON);

      var response = restTemplate.exchange(
          baseUrl() + "/media-123/resize",
          HttpMethod.PUT,
          new HttpEntity<>("{\"width\": 800}", headers),
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    }
  }

  @Nested
  @DisplayName("Get All Media")
  class GetAllMedia {

    @Test
    @DisplayName("should return paginated media")
    void shouldReturnPaginatedMedia() {
      createAndSaveMedia("media-1", MediaStatus.COMPLETE);
      createAndSaveMedia("media-2", MediaStatus.PROCESSING);

      var response = restTemplate.getForEntity(baseUrl(), String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
      assertThat(response.getBody())
          .contains("items")
          .contains("media-1")
          .contains("media-2")
          .contains("hasMore");
    }

    @Test
    @DisplayName("should paginate with limit parameter")
    void shouldPaginateWithLimit() {
      createAndSaveMedia("media-a", MediaStatus.COMPLETE);
      createAndSaveMedia("media-b", MediaStatus.COMPLETE);
      createAndSaveMedia("media-c", MediaStatus.COMPLETE);

      var response = restTemplate.getForEntity(baseUrl() + "?limit=2", String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
      assertThat(response.getBody())
          .contains("items")
          .contains("hasMore")
          .contains("nextCursor");
    }
  }

  @Nested
  @DisplayName("Presigned Upload")
  class PresignedUpload {

    @Test
    @DisplayName("should initialize presigned upload")
    void shouldInitializePresignedUpload() {
      var headers = new HttpHeaders();
      headers.setContentType(MediaType.APPLICATION_JSON);

      var response = restTemplate.postForEntity(
          baseUrl() + "/upload/init",
          new HttpEntity<>("""
              {
                "fileName": "large-image.jpg",
                "fileSize": 52428800,
                "contentType": "image/jpeg",
                "width": 800
              }
              """, headers),
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
      assertThat(response.getBody())
          .contains("mediaId")
          .contains("uploadUrl")
          .contains("expiresIn")
          .contains("method");
    }

    @Test
    @DisplayName("should reject non-image content type")
    void shouldRejectNonImageContentType() {
      var headers = new HttpHeaders();
      headers.setContentType(MediaType.APPLICATION_JSON);

      var response = restTemplate.postForEntity(
          baseUrl() + "/upload/init",
          new HttpEntity<>("""
              {
                "fileName": "document.pdf",
                "fileSize": 1024,
                "contentType": "application/pdf"
              }
              """, headers),
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
      assertThat(response.getBody()).contains("Only images are supported");
    }

    @Test
    @DisplayName("should complete upload when file exists in S3")
    void shouldCompleteUploadWhenFileExists() {
      // Create media in PENDING_UPLOAD status
      createAndSaveMedia("media-upload-123", MediaStatus.PENDING_UPLOAD);

      // Upload file to S3
      s3Client.putObject(
          b -> b.bucket("media-bucket").key("uploads/media-upload-123/test.jpg").contentType("image/jpeg"),
          software.amazon.awssdk.core.sync.RequestBody.fromBytes("test-content".getBytes()));

      var response = restTemplate.postForEntity(
          baseUrl() + "/media-upload-123/upload/complete",
          null,
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
      assertThat(response.getBody()).contains("media-upload-123");

      // Verify status changed to PENDING
      var media = dynamoDbService.getMedia("media-upload-123");
      assertThat(media).isPresent();
      assertThat(media.get().getStatus()).isEqualTo(MediaStatus.PENDING);
    }

    @Test
    @DisplayName("should return 400 when file not uploaded to S3")
    void shouldReturn400WhenFileNotUploaded() {
      // Create media in PENDING_UPLOAD status but don't upload file
      createAndSaveMedia("media-no-file", MediaStatus.PENDING_UPLOAD);

      var response = restTemplate.postForEntity(
          baseUrl() + "/media-no-file/upload/complete",
          null,
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }

    @Test
    @DisplayName("should return 400 when media not in PENDING_UPLOAD status")
    void shouldReturn400WhenWrongStatus() {
      createAndSaveMedia("media-wrong-status", MediaStatus.COMPLETE);

      var response = restTemplate.postForEntity(
          baseUrl() + "/media-wrong-status/upload/complete",
          null,
          String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    }
  }

  private Media createAndSaveMedia(String mediaId, MediaStatus status) {
    var media = Media.builder()
        .mediaId(mediaId)
        .name("test.jpg")
        .size(1024L)
        .mimetype("image/jpeg")
        .status(status)
        .width(500)
        .outputFormat(OutputFormat.JPEG)
        .build();
    dynamoDbService.createMedia(media);
    if (status != MediaStatus.PENDING) {
      dynamoDbService.updateStatus(mediaId, status);
    }
    return media;
  }

  /**
   * Creates a small test image in the specified format.
   */
  private byte[] createTestImage(String format) throws IOException {
    BufferedImage image = new BufferedImage(100, 100, BufferedImage.TYPE_INT_RGB);
    var g = image.getGraphics();
    g.fillRect(0, 0, 100, 100);
    g.dispose();

    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    ImageIO.write(image, format, baos);
    return baos.toByteArray();
  }
}
