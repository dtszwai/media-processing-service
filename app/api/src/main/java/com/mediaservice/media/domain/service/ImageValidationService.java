package com.mediaservice.media.domain.service;

import com.mediaservice.shared.http.error.InvalidImageException;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Set;

/**
 * Service for validating image file content.
 * Performs actual image parsing to verify file integrity,
 * not just MIME type checking which can be spoofed.
 */
@Slf4j
@Service
public class ImageValidationService {

  private static final Set<String> SUPPORTED_FORMATS = Set.of("jpeg", "jpg", "png", "gif", "webp", "bmp");
  private static final int MAX_DIMENSION = 8192;  // 8K max dimension (memory-safe for Lambda)
  private static final int MIN_DIMENSION = 1;
  private static final long MAX_FILE_SIZE = 50 * 1024 * 1024; // 50MB max file size

  // Magic bytes for common image formats
  private static final byte[] JPEG_MAGIC = new byte[] { (byte) 0xFF, (byte) 0xD8, (byte) 0xFF };
  private static final byte[] PNG_MAGIC = new byte[] { (byte) 0x89, 0x50, 0x4E, 0x47 };
  private static final byte[] GIF_MAGIC = new byte[] { 0x47, 0x49, 0x46, 0x38 };
  private static final byte[] WEBP_MAGIC = new byte[] { 0x52, 0x49, 0x46, 0x46 }; // RIFF header
  private static final byte[] BMP_MAGIC = new byte[] { 0x42, 0x4D };

  /**
   * Validates that the uploaded file is a valid image.
   *
   * @param file the uploaded file
   * @throws InvalidImageException if the file is not a valid image
   */
  public void validateImage(MultipartFile file) throws InvalidImageException {
    if (file == null || file.isEmpty()) {
      throw new InvalidImageException("File is empty or null");
    }

    try {
      byte[] bytes = file.getBytes();
      validateImageBytes(bytes, file.getOriginalFilename());
    } catch (IOException e) {
      log.error("Failed to read file bytes: {}", e.getMessage());
      throw new InvalidImageException("Failed to read file content");
    }
  }

  // Formats that Java's ImageIO can parse natively (without additional libraries)
  private static final Set<String> IMAGEIO_NATIVE_FORMATS = Set.of("jpeg", "png", "gif", "bmp");

  /**
   * Validates image bytes (for presigned upload completion where we fetch from
   * S3).
   *
   * @param bytes    the image bytes
   * @param fileName the original file name (for logging)
   * @throws InvalidImageException if the bytes don't represent a valid image
   */
  public void validateImageBytes(byte[] bytes, String fileName) throws InvalidImageException {
    if (bytes == null || bytes.length == 0) {
      throw new InvalidImageException("Image data is empty");
    }

    // Step 0: Check file size
    if (bytes.length > MAX_FILE_SIZE) {
      throw new InvalidImageException(
          "File size too large: %d bytes (maximum: %d bytes / %d MB)"
              .formatted(bytes.length, MAX_FILE_SIZE, MAX_FILE_SIZE / (1024 * 1024)));
    }

    // Step 1: Check magic bytes to verify file type matches content
    String detectedFormat = detectImageFormat(bytes);
    if (detectedFormat == null) {
      throw new InvalidImageException("File does not appear to be a valid image (invalid magic bytes)");
    }
    log.debug("Detected image format: {} for file: {}", detectedFormat, fileName);

    // Step 2: For formats ImageIO supports natively, do full parsing validation
    // For other formats (WebP, etc.), trust magic byte detection
    BufferedImage image = parseImage(bytes);
    if (image == null) {
      if (IMAGEIO_NATIVE_FORMATS.contains(detectedFormat)) {
        // Native format should parse - if it doesn't, file is corrupted
        throw new InvalidImageException("File could not be parsed as a valid image");
      }
      // Non-native format (WebP, etc.) - trust magic bytes, skip dimension check
      log.info("Image validation passed (magic bytes only): file={}, format={}", fileName, detectedFormat);
      return;
    }

    // Step 3: Validate dimensions (only if we could parse the image)
    validateDimensions(image, fileName);

    log.info("Image validation passed: file={}, format={}, dimensions={}x{}",
        fileName, detectedFormat, image.getWidth(), image.getHeight());
  }

  /**
   * Detects image format from magic bytes.
   *
   * @param bytes the file bytes
   * @return detected format name, or null if not recognized
   */
  private String detectImageFormat(byte[] bytes) {
    if (bytes.length < 12) {
      return null;
    }

    if (startsWith(bytes, JPEG_MAGIC)) {
      return "jpeg";
    }
    if (startsWith(bytes, PNG_MAGIC)) {
      return "png";
    }
    if (startsWith(bytes, GIF_MAGIC)) {
      return "gif";
    }
    if (startsWith(bytes, BMP_MAGIC)) {
      return "bmp";
    }
    // WebP has RIFF header followed by WEBP at offset 8
    if (startsWith(bytes, WEBP_MAGIC) && bytes.length >= 12) {
      if (bytes[8] == 'W' && bytes[9] == 'E' && bytes[10] == 'B' && bytes[11] == 'P') {
        return "webp";
      }
    }

    return null;
  }

  private boolean startsWith(byte[] data, byte[] prefix) {
    if (data.length < prefix.length) {
      return false;
    }
    for (int i = 0; i < prefix.length; i++) {
      if (data[i] != prefix[i]) {
        return false;
      }
    }
    return true;
  }

  /**
   * Attempts to parse the bytes as an image using ImageIO.
   *
   * @param bytes the image bytes
   * @return BufferedImage if successful, null otherwise
   */
  private BufferedImage parseImage(byte[] bytes) {
    try (InputStream is = new ByteArrayInputStream(bytes)) {
      return ImageIO.read(is);
    } catch (IOException e) {
      log.warn("Failed to parse image: {}", e.getMessage());
      return null;
    }
  }

  /**
   * Validates image dimensions are within acceptable bounds.
   *
   * @param image    the parsed image
   * @param fileName the file name for logging
   * @throws InvalidImageException if dimensions are invalid
   */
  private void validateDimensions(BufferedImage image, String fileName) throws InvalidImageException {
    int width = image.getWidth();
    int height = image.getHeight();

    if (width < MIN_DIMENSION || height < MIN_DIMENSION) {
      throw new InvalidImageException(
          "Image dimensions too small: %dx%d (minimum: %dx%d)"
              .formatted(width, height, MIN_DIMENSION, MIN_DIMENSION));
    }

    if (width > MAX_DIMENSION || height > MAX_DIMENSION) {
      throw new InvalidImageException(
          "Image dimensions too large: %dx%d (maximum: %dx%d)"
              .formatted(width, height, MAX_DIMENSION, MAX_DIMENSION));
    }
  }

  /**
   * Checks if the given content type is a supported image type.
   *
   * @param contentType the MIME content type
   * @return true if supported
   */
  public boolean isSupportedContentType(String contentType) {
    if (contentType == null) {
      return false;
    }
    String type = contentType.toLowerCase();
    return type.startsWith("image/") && SUPPORTED_FORMATS.stream()
        .anyMatch(format -> type.contains(format));
  }
}
