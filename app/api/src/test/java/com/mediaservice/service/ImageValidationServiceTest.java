package com.mediaservice.service;

import com.mediaservice.exception.InvalidImageException;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.mock.web.MockMultipartFile;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.io.IOException;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class ImageValidationServiceTest {
  private ImageValidationService validationService;

  @BeforeEach
  void setUp() {
    validationService = new ImageValidationService();
  }

  @Nested
  @DisplayName("validateImage")
  class ValidateImage {
    @Test
    @DisplayName("should accept valid JPEG image")
    void shouldAcceptValidJpeg() throws IOException {
      byte[] jpegBytes = createTestImage("jpg");
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", jpegBytes);
      // Should not throw
      validationService.validateImage(file);
    }

    @Test
    @DisplayName("should accept valid PNG image")
    void shouldAcceptValidPng() throws IOException {
      byte[] pngBytes = createTestImage("png");
      var file = new MockMultipartFile("file", "test.png", "image/png", pngBytes);
      validationService.validateImage(file);
    }

    @Test
    @DisplayName("should reject empty file")
    void shouldRejectEmptyFile() {
      var file = new MockMultipartFile("file", "test.jpg", "image/jpeg", new byte[0]);
      assertThatThrownBy(() -> validationService.validateImage(file))
          .isInstanceOf(InvalidImageException.class)
          .hasMessageContaining("empty");
    }

    @Test
    @DisplayName("should reject null file")
    void shouldRejectNullFile() {
      assertThatThrownBy(() -> validationService.validateImage(null))
          .isInstanceOf(InvalidImageException.class)
          .hasMessageContaining("empty or null");
    }

    @Test
    @DisplayName("should reject file with invalid magic bytes")
    void shouldRejectInvalidMagicBytes() {
      byte[] invalidBytes = "This is not an image".getBytes();
      var file = new MockMultipartFile("file", "fake.jpg", "image/jpeg", invalidBytes);

      assertThatThrownBy(() -> validationService.validateImage(file))
          .isInstanceOf(InvalidImageException.class)
          .hasMessageContaining("invalid magic bytes");
    }

    @Test
    @DisplayName("should reject corrupted image with valid magic bytes")
    void shouldRejectCorruptedImage() {
      // JPEG magic bytes but corrupted content
      byte[] corruptedJpeg = new byte[] { (byte) 0xFF, (byte) 0xD8, (byte) 0xFF, (byte) 0xE0,
          0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00 };
      var file = new MockMultipartFile("file", "corrupted.jpg", "image/jpeg", corruptedJpeg);
      assertThatThrownBy(() -> validationService.validateImage(file))
          .isInstanceOf(InvalidImageException.class)
          .hasMessageContaining("could not be parsed");
    }
  }

  @Nested
  @DisplayName("validateImageBytes")
  class ValidateImageBytes {
    @Test
    @DisplayName("should accept valid image bytes")
    void shouldAcceptValidImageBytes() throws IOException {
      byte[] imageBytes = createTestImage("png");
      // Should not throw
      validationService.validateImageBytes(imageBytes, "test.png");
    }

    @Test
    @DisplayName("should reject null bytes")
    void shouldRejectNullBytes() {
      assertThatThrownBy(() -> validationService.validateImageBytes(null, "test.jpg"))
          .isInstanceOf(InvalidImageException.class)
          .hasMessageContaining("empty");
    }

    @Test
    @DisplayName("should reject empty bytes")
    void shouldRejectEmptyBytes() {
      assertThatThrownBy(() -> validationService.validateImageBytes(new byte[0], "test.jpg"))
          .isInstanceOf(InvalidImageException.class)
          .hasMessageContaining("empty");
    }
  }

  @Nested
  @DisplayName("isSupportedContentType")
  class IsSupportedContentType {
    @Test
    @DisplayName("should return true for supported types")
    void shouldReturnTrueForSupportedTypes() {
      assertThat(validationService.isSupportedContentType("image/jpeg")).isTrue();
      assertThat(validationService.isSupportedContentType("image/png")).isTrue();
      assertThat(validationService.isSupportedContentType("image/gif")).isTrue();
      assertThat(validationService.isSupportedContentType("image/webp")).isTrue();
      assertThat(validationService.isSupportedContentType("image/bmp")).isTrue();
    }

    @Test
    @DisplayName("should return false for unsupported types")
    void shouldReturnFalseForUnsupportedTypes() {
      assertThat(validationService.isSupportedContentType("image/svg+xml")).isFalse();
      assertThat(validationService.isSupportedContentType("application/pdf")).isFalse();
      assertThat(validationService.isSupportedContentType("text/plain")).isFalse();
    }

    @Test
    @DisplayName("should return false for null")
    void shouldReturnFalseForNull() {
      assertThat(validationService.isSupportedContentType(null)).isFalse();
    }
  }

  /**
   * Creates a small test image in the specified format.
   */
  private byte[] createTestImage(String format) throws IOException {
    BufferedImage image = new BufferedImage(100, 100, BufferedImage.TYPE_INT_RGB);
    // Draw something to ensure it's not empty
    var g = image.getGraphics();
    g.fillRect(0, 0, 100, 100);
    g.dispose();

    ByteArrayOutputStream baos = new ByteArrayOutputStream();
    ImageIO.write(image, format, baos);
    return baos.toByteArray();
  }
}
