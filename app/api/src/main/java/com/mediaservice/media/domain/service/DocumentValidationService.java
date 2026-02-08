package com.mediaservice.media.domain.service;

import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.IOException;

/**
 * Service for validating document uploads.
 * Currently supports basic PDF validation via magic bytes.
 */
@Slf4j
@Service
public class DocumentValidationService {
  private static final byte[] PDF_MAGIC = new byte[] { 0x25, 0x50, 0x44, 0x46, 0x2D }; // "%PDF-"

  public void validatePdf(MultipartFile file) {
    if (file == null || file.isEmpty()) {
      throw new IllegalArgumentException("File is empty or null");
    }

    try {
      byte[] bytes = file.getBytes();
      if (!startsWith(bytes, PDF_MAGIC)) {
        throw new IllegalArgumentException("File does not appear to be a valid PDF");
      }
      log.info("PDF validation passed: file={}", file.getOriginalFilename());
    } catch (IOException e) {
      log.warn("Failed to read PDF bytes: {}", e.getMessage());
      throw new IllegalArgumentException("Failed to read file content");
    }
  }

  private boolean startsWith(byte[] data, byte[] prefix) {
    if (data == null || data.length < prefix.length) {
      return false;
    }
    for (int i = 0; i < prefix.length; i++) {
      if (data[i] != prefix[i]) {
        return false;
      }
    }
    return true;
  }
}
