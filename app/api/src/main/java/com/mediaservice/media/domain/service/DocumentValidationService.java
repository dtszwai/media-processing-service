package com.mediaservice.media.domain.service;

import lombok.extern.slf4j.Slf4j;
import com.mediaservice.shared.config.properties.MediaProperties;
import org.apache.pdfbox.io.MemoryUsageSetting;
import org.apache.pdfbox.pdmodel.PDDocument;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.ByteArrayInputStream;
import java.io.IOException;

/**
 * Service for validating document uploads.
 * Currently supports basic PDF validation via magic bytes.
 */
@Slf4j
@Service
public class DocumentValidationService {
  private static final byte[] PDF_MAGIC = new byte[] { 0x25, 0x50, 0x44, 0x46, 0x2D }; // "%PDF-"
  private final MediaProperties mediaProperties;

  public DocumentValidationService(MediaProperties mediaProperties) {
    this.mediaProperties = mediaProperties;
  }

  public void validatePdf(MultipartFile file) {
    if (file == null || file.isEmpty()) {
      throw new IllegalArgumentException("File is empty or null");
    }

    try {
      byte[] bytes = file.getBytes();
      if (!startsWith(bytes, PDF_MAGIC)) {
        throw new IllegalArgumentException("File does not appear to be a valid PDF");
      }
      try (PDDocument document = PDDocument.load(new ByteArrayInputStream(bytes), MemoryUsageSetting.setupTempFileOnly())) {
        if (document.isEncrypted()) {
          throw new IllegalArgumentException("Encrypted PDFs are not supported");
        }
        int pageCount = document.getNumberOfPages();
        if (pageCount < 1) {
          throw new IllegalArgumentException("PDF has no pages");
        }
        int maxPages = mediaProperties.getDocument().getMaxPages();
        if (pageCount > maxPages) {
          throw new IllegalArgumentException("PDF exceeds maximum page limit of " + maxPages);
        }
      }
      log.info("PDF validation passed: file={}, size={}, pages<=max", file.getOriginalFilename(), file.getSize());
    } catch (IOException e) {
      log.warn("Failed to read or parse PDF: {}", e.getMessage());
      throw new IllegalArgumentException("Failed to read or parse PDF");
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
