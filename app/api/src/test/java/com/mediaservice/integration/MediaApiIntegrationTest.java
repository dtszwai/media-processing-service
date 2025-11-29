package com.mediaservice.integration;

import com.mediaservice.model.Media;
import com.mediaservice.model.MediaStatus;
import com.mediaservice.service.DynamoDbService;
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

import static org.assertj.core.api.Assertions.assertThat;

@Tag("integration")
class MediaApiIntegrationTest extends BaseIntegrationTest {

  @LocalServerPort
  private int port;

  @Autowired
  private TestRestTemplate restTemplate;

  @Autowired
  private DynamoDbService dynamoDbService;

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
    void shouldUploadImage() {
      var headers = new HttpHeaders();
      headers.setContentType(MediaType.MULTIPART_FORM_DATA);

      var body = new LinkedMultiValueMap<String, Object>();
      body.add("file", new ByteArrayResource("test-image-content".getBytes()) {
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
      s3Client.putObject(
          b -> b.bucket("media-bucket").key("resized/media-123/test.jpg").contentType("image/jpeg"),
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
    @DisplayName("should return all media")
    void shouldReturnAllMedia() {
      createAndSaveMedia("media-1", MediaStatus.COMPLETE);
      createAndSaveMedia("media-2", MediaStatus.PROCESSING);

      var response = restTemplate.getForEntity(baseUrl(), String.class);

      assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
      assertThat(response.getBody()).contains("media-1").contains("media-2");
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
        .build();
    dynamoDbService.createMedia(media);
    if (status != MediaStatus.PENDING) {
      dynamoDbService.updateStatus(mediaId, status);
    }
    return media;
  }
}
