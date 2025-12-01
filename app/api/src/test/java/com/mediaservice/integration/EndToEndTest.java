package com.mediaservice.integration;

import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import org.junit.jupiter.api.DisplayName;
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

import static org.assertj.core.api.Assertions.assertThat;

/**
 * End-to-end tests for the complete media processing workflow.
 * Tests the full lifecycle: upload -> status check -> download
 */
@Tag("integration")
class EndToEndTest extends BaseIntegrationTest {

  @LocalServerPort
  private int port;

  @Autowired
  private TestRestTemplate restTemplate;

  @Autowired
  private MediaDynamoDbRepository dynamoDbService;

  private String baseUrl() {
    return "http://localhost:" + port + "/v1/media";
  }

  @Test
  @DisplayName("Complete upload workflow: upload -> verify storage -> check status")
  void completeUploadWorkflow() throws Exception {
    // Create a real JPEG image
    byte[] imageData = createTestJpegImage(800, 600);

    // Step 1: Upload the image
    var headers = new HttpHeaders();
    headers.setContentType(MediaType.MULTIPART_FORM_DATA);

    var body = new LinkedMultiValueMap<String, Object>();
    body.add("file", new ByteArrayResource(imageData) {
      @Override
      public String getFilename() {
        return "test-image.jpg";
      }
    });

    var uploadResponse = restTemplate.postForEntity(
        baseUrl() + "/upload",
        new HttpEntity<>(body, headers),
        String.class);

    assertThat(uploadResponse.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    assertThat(uploadResponse.getBody()).contains("mediaId");

    // Extract mediaId from response
    String responseBody = uploadResponse.getBody();
    String mediaId = extractMediaId(responseBody);

    // Step 2: Verify media was stored in DynamoDB
    var mediaOpt = dynamoDbService.getMedia(mediaId);
    assertThat(mediaOpt).isPresent();
    assertThat(mediaOpt.get().getName()).isEqualTo("test-image.jpg");
    assertThat(mediaOpt.get().getMimetype()).isEqualTo("image/jpeg");
    assertThat(mediaOpt.get().getStatus()).isEqualTo(MediaStatus.PENDING);

    // Step 3: Verify file was uploaded to S3 - key format: {mediaId}/original.{ext}
    var objects = s3Client.listObjectsV2(b -> b
        .bucket("media-bucket")
        .prefix(mediaId + "/"));
    assertThat(objects.contents()).hasSize(1);

    // Step 4: Check status via API
    var statusResponse = restTemplate.getForEntity(
        baseUrl() + "/" + mediaId + "/status", String.class);
    assertThat(statusResponse.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(statusResponse.getBody()).contains("PENDING");
  }

  @Test
  @DisplayName("Upload with custom width parameter")
  void uploadWithCustomWidth() throws Exception {
    byte[] imageData = createTestJpegImage(1000, 800);

    var headers = new HttpHeaders();
    headers.setContentType(MediaType.MULTIPART_FORM_DATA);

    var body = new LinkedMultiValueMap<String, Object>();
    body.add("file", new ByteArrayResource(imageData) {
      @Override
      public String getFilename() {
        return "test.jpg";
      }
    });

    var response = restTemplate.postForEntity(
        baseUrl() + "/upload?width=300",
        new HttpEntity<>(body, headers),
        String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);

    String mediaId = extractMediaId(response.getBody());
    var media = dynamoDbService.getMedia(mediaId);
    assertThat(media.get().getWidth()).isEqualTo(300);
  }

  @Test
  @DisplayName("Full CRUD lifecycle")
  void fullCrudLifecycle() throws Exception {
    byte[] imageData = createTestJpegImage(500, 400);

    // CREATE
    var headers = new HttpHeaders();
    headers.setContentType(MediaType.MULTIPART_FORM_DATA);
    var body = new LinkedMultiValueMap<String, Object>();
    body.add("file", new ByteArrayResource(imageData) {
      @Override
      public String getFilename() {
        return "lifecycle-test.jpg";
      }
    });

    var createResponse = restTemplate.postForEntity(
        baseUrl() + "/upload",
        new HttpEntity<>(body, headers),
        String.class);
    String mediaId = extractMediaId(createResponse.getBody());

    // READ
    var getResponse = restTemplate.getForEntity(
        baseUrl() + "/" + mediaId, String.class);
    assertThat(getResponse.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(getResponse.getBody())
        .contains(mediaId)
        .contains("lifecycle-test.jpg");

    // DELETE (soft delete)
    var deleteResponse = restTemplate.exchange(
        baseUrl() + "/" + mediaId,
        HttpMethod.DELETE,
        null,
        String.class);
    assertThat(deleteResponse.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);

    // Verify status changed to DELETED (soft delete preserves record)
    var statusAfterDelete = dynamoDbService.getMedia(mediaId);
    assertThat(statusAfterDelete).isPresent();
    assertThat(statusAfterDelete.get().getStatus()).isEqualTo(MediaStatus.DELETED);
    assertThat(statusAfterDelete.get().getDeletedAt()).isNotNull();
  }

  @Test
  @DisplayName("Concurrent uploads should all succeed")
  void concurrentUploads() throws Exception {
    int uploadCount = 5;
    var latch = new java.util.concurrent.CountDownLatch(uploadCount);
    var successCount = new java.util.concurrent.atomic.AtomicInteger(0);

    try (var executor = java.util.concurrent.Executors.newFixedThreadPool(uploadCount)) {
      for (int i = 0; i < uploadCount; i++) {
        final int index = i;
        executor.submit(() -> {
          try {
            byte[] imageData = createTestJpegImage(200 + index * 50, 150 + index * 50);

            var headers = new HttpHeaders();
            headers.setContentType(MediaType.MULTIPART_FORM_DATA);
            var body = new LinkedMultiValueMap<String, Object>();
            body.add("file", new ByteArrayResource(imageData) {
              @Override
              public String getFilename() {
                return "concurrent-" + index + ".jpg";
              }
            });

            var response = restTemplate.postForEntity(
                baseUrl() + "/upload",
                new HttpEntity<>(body, headers),
                String.class);

            if (response.getStatusCode() == HttpStatus.ACCEPTED) {
              successCount.incrementAndGet();
            }
          } catch (Exception e) {
            // Log but don't fail - count will be checked
          } finally {
            latch.countDown();
          }
        });
      }
      latch.await();
    }

    assertThat(successCount.get()).isEqualTo(uploadCount);

    // Verify all media entries were created
    var result = dynamoDbService.getMediaPaginated(null, uploadCount + 1);
    assertThat(result.items()).hasSize(uploadCount);
  }

  @Test
  @DisplayName("List all media endpoint returns correct count")
  void listAllMedia() throws Exception {
    // Create multiple media entries
    for (int i = 0; i < 3; i++) {
      byte[] imageData = createTestJpegImage(300, 200);
      var headers = new HttpHeaders();
      headers.setContentType(MediaType.MULTIPART_FORM_DATA);
      var body = new LinkedMultiValueMap<String, Object>();
      final int index = i;
      body.add("file", new ByteArrayResource(imageData) {
        @Override
        public String getFilename() {
          return "list-test-" + index + ".jpg";
        }
      });

      restTemplate.postForEntity(
          baseUrl() + "/upload",
          new HttpEntity<>(body, headers),
          String.class);
    }

    // Get all media (paginated response)
    var response = restTemplate.getForEntity(baseUrl(), String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    // Should contain all 3 uploads in paginated format
    assertThat(response.getBody())
        .contains("items")
        .contains("list-test-0.jpg")
        .contains("list-test-1.jpg")
        .contains("list-test-2.jpg");
  }

  private byte[] createTestJpegImage(int width, int height) throws Exception {
    BufferedImage image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
    // Create a simple gradient image
    for (int x = 0; x < width; x++) {
      for (int y = 0; y < height; y++) {
        int r = (x * 255) / width;
        int g = (y * 255) / height;
        int b = 128;
        image.setRGB(x, y, (r << 16) | (g << 8) | b);
      }
    }
    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    ImageIO.write(image, "jpg", baos);
    return baos.toByteArray();
  }

  private String extractMediaId(String jsonResponse) {
    // Simple extraction - in production use Jackson
    int start = jsonResponse.indexOf("\"mediaId\":\"") + 11;
    int end = jsonResponse.indexOf("\"", start);
    return jsonResponse.substring(start, end);
  }
}
