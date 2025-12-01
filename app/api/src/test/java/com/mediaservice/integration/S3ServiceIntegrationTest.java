package com.mediaservice.integration;

import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.media.infrastructure.storage.S3StorageService;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.mock.web.MockMultipartFile;
import software.amazon.awssdk.services.s3.model.GetObjectRequest;
import software.amazon.awssdk.services.s3.model.ListObjectsV2Request;

import java.time.Duration;

import static org.assertj.core.api.Assertions.assertThat;

@Tag("integration")
class S3StorageServiceIntegrationTest extends BaseIntegrationTest {

  @Autowired
  private S3StorageService s3Service;

  @Nested
  @DisplayName("File Upload")
  class FileUpload {

    @Test
    @DisplayName("should upload file to S3")
    void shouldUploadFile() throws Exception {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", "test-content".getBytes());
      s3Service.uploadMedia("media-123", "test.jpg", file);
      var objects = s3Client.listObjectsV2(ListObjectsV2Request.builder()
          .bucket("media-bucket")
          .prefix("uploads/media-123/")
          .build());

      assertThat(objects.contents()).hasSize(1);
      assertThat(objects.contents().get(0).key()).isEqualTo("uploads/media-123/test.jpg");
    }

    @Test
    @DisplayName("should preserve content type")
    void shouldPreserveContentType() throws Exception {
      var file = new MockMultipartFile(
          "file", "test.png", "image/png", "png-content".getBytes());

      s3Service.uploadMedia("media-123", "test.png", file);

      try (var response = s3Client.getObject(GetObjectRequest.builder()
          .bucket("media-bucket")
          .key("uploads/media-123/test.png")
          .build())) {
        assertThat(response.response().contentType()).isEqualTo("image/png");
      }
    }
  }

  @Nested
  @DisplayName("Presigned URL")
  class PresignedUrl {

    @Test
    @DisplayName("should generate presigned URL for resized file")
    void shouldGeneratePresignedUrl() throws Exception {
      // Upload a file to the resized folder first
      new MockMultipartFile("file", "test.jpg", "image/jpeg", "test-content".getBytes());
      s3Client.putObject(
          b -> b.bucket("media-bucket").key("resized/media-123/test.jpeg").contentType("image/jpeg"),
          software.amazon.awssdk.core.sync.RequestBody.fromBytes("test-content".getBytes()));
      var url = s3Service.getPresignedUrl("media-123", "test.jpg", OutputFormat.JPEG);
      assertThat(url)
          .isNotBlank()
          .contains("media-bucket")
          .contains("resized/media-123/test.jpeg");
    }
  }

  @Nested
  @DisplayName("Presigned Upload URL")
  class PresignedUploadUrl {

    @Test
    @DisplayName("should generate presigned upload URL")
    void shouldGeneratePresignedUploadUrl() {
      var url = s3Service.generatePresignedUploadUrl(
          "media-456", "large-image.jpg", "image/jpeg", Duration.ofHours(1));

      assertThat(url)
          .isNotBlank()
          .contains("media-bucket")
          .contains("uploads/media-456/large-image.jpg");
    }

    @Test
    @DisplayName("should generate different URLs for different files")
    void shouldGenerateDifferentUrls() {
      var url1 = s3Service.generatePresignedUploadUrl(
          "media-1", "file1.jpg", "image/jpeg", Duration.ofHours(1));
      var url2 = s3Service.generatePresignedUploadUrl(
          "media-2", "file2.jpg", "image/jpeg", Duration.ofHours(1));

      assertThat(url1).isNotEqualTo(url2);
      assertThat(url1).contains("media-1");
      assertThat(url2).contains("media-2");
    }
  }

  @Nested
  @DisplayName("Object Exists")
  class ObjectExists {

    @Test
    @DisplayName("should return true when object exists")
    void shouldReturnTrueWhenExists() {
      // Upload a file first
      s3Client.putObject(
          b -> b.bucket("media-bucket").key("uploads/media-exists/test.jpg").contentType("image/jpeg"),
          software.amazon.awssdk.core.sync.RequestBody.fromBytes("test-content".getBytes()));

      var exists = s3Service.objectExists("media-exists", "test.jpg");

      assertThat(exists).isTrue();
    }

    @Test
    @DisplayName("should return false when object does not exist")
    void shouldReturnFalseWhenNotExists() {
      var exists = s3Service.objectExists("nonexistent-media", "nonexistent.jpg");

      assertThat(exists).isFalse();
    }
  }
}
