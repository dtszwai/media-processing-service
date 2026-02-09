package com.mediaservice.lambda.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.lambda.config.LambdaConfig;
import net.coobird.thumbnailator.Thumbnails;
import net.coobird.thumbnailator.geometry.Positions;
import org.apache.pdfbox.io.MemoryUsageSetting;
import org.apache.pdfbox.pdmodel.PDDocument;
import org.apache.pdfbox.pdmodel.PDDocumentInformation;
import org.apache.pdfbox.rendering.ImageType;
import org.apache.pdfbox.rendering.PDFRenderer;
import org.apache.pdfbox.text.PDFTextStripper;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Calendar;
import java.util.List;

public class DocumentProcessingService {
  private static final Logger logger = LoggerFactory.getLogger(DocumentProcessingService.class);

  static {
    System.setProperty("java.awt.headless", "true");
  }

  private final LambdaConfig config;
  private final ObjectMapper objectMapper;
  private final BufferedImage watermarkImage;

  public DocumentProcessingService(ObjectMapper objectMapper) {
    this.config = LambdaConfig.getInstance();
    this.objectMapper = objectMapper;
    this.watermarkImage = loadWatermark();
  }

  public DocumentProcessingResult process(String mediaId, byte[] pdfBytes) throws IOException {
    try (PDDocument document = PDDocument.load(new ByteArrayInputStream(pdfBytes), MemoryUsageSetting.setupTempFileOnly())) {
      if (document.isEncrypted()) {
        throw new IllegalArgumentException("Encrypted PDFs are not supported");
      }
      int pageCount = document.getNumberOfPages();
      if (pageCount < 1) {
        throw new IllegalArgumentException("PDF has no pages");
      }
      int maxPages = config.getDocumentMaxPages();
      if (pageCount > maxPages) {
        throw new IllegalArgumentException("PDF exceeds maximum page limit of " + maxPages);
      }

      DocumentMetadata metadata = extractMetadata(document, pageCount);
      TextExtractionResult textResult = extractText(mediaId, document, pageCount);
      byte[] previewBytes = renderPreview(document);

      return new DocumentProcessingResult(previewBytes, textResult.textJson(), metadata.withTextStats(textResult));
    }
  }

  private DocumentMetadata extractMetadata(PDDocument document, int pageCount) {
    PDDocumentInformation info = document.getDocumentInformation();
    return new DocumentMetadata(
        pageCount,
        info.getTitle(),
        info.getAuthor(),
        info.getSubject(),
        info.getCreator(),
        info.getProducer(),
        toInstant(info.getCreationDate()),
        toInstant(info.getModificationDate()),
        null,
        null
    );
  }

  private TextExtractionResult extractText(String mediaId, PDDocument document, int pageCount) throws IOException {
    int maxChars = config.getDocumentMaxTextChars();
    int totalChars = 0;
    boolean truncated = false;

    var stripper = new PDFTextStripper();
    List<DocumentTextPage> pages = new ArrayList<>();

    for (int i = 1; i <= pageCount; i++) {
      stripper.setStartPage(i);
      stripper.setEndPage(i);
      String text = stripper.getText(document);
      if (text == null) {
        text = "";
      } else {
        text = text.replace("\u0000", "");
      }

      if (totalChars + text.length() > maxChars) {
        int remaining = Math.max(0, maxChars - totalChars);
        text = text.substring(0, remaining);
        truncated = true;
      }

      pages.add(new DocumentTextPage(i, text));
      totalChars += text.length();

      if (truncated) {
        break;
      }
    }

    var payload = new DocumentTextPayload(
        mediaId,
        pageCount,
        Instant.now().toString(),
        truncated,
        pages
    );
    byte[] json = objectMapper.writeValueAsBytes(payload);
    return new TextExtractionResult(json, totalChars, truncated);
  }

  private byte[] renderPreview(PDDocument document) throws IOException {
    PDFRenderer renderer = new PDFRenderer(document);
    BufferedImage pageImage = renderer.renderImageWithDPI(0, config.getDocumentPreviewDpi(), ImageType.RGB);

    int targetWidth = Math.min(StorageConstants.PREVIEW_MAX_WIDTH, pageImage.getWidth());
    int watermarkWidth = Math.max(
        (int) (targetWidth * config.getWatermarkWidthRatio()),
        config.getMinWatermarkWidth() * 2);
    BufferedImage resizedWatermark = Thumbnails.of(watermarkImage).width(watermarkWidth).asBufferedImage();

    var outputStream = new ByteArrayOutputStream();
    Thumbnails.of(pageImage)
        .width(targetWidth)
        .outputFormat("png")
        .watermark(Positions.CENTER, resizedWatermark, 0.5f)
        .outputQuality(StorageConstants.PREVIEW_QUALITY)
        .toOutputStream(outputStream);
    return outputStream.toByteArray();
  }

  private BufferedImage loadWatermark() {
    try (var watermarkStream = DocumentProcessingService.class.getResourceAsStream("/media-service-watermark.png")) {
      if (watermarkStream == null) {
        throw new IllegalStateException("Watermark image not found at /media-service-watermark.png");
      }
      var image = ImageIO.read(watermarkStream);
      if (image == null) {
        throw new IllegalStateException("Failed to decode watermark image");
      }
      return image;
    } catch (IOException e) {
      throw new IllegalStateException("Failed to load watermark image", e);
    }
  }

  private Instant toInstant(Calendar calendar) {
    return calendar != null ? calendar.toInstant() : null;
  }

  public record DocumentProcessingResult(byte[] previewPng, byte[] textJson, DocumentMetadata metadata) {}

  public record DocumentTextPayload(
      String mediaId,
      int pageCount,
      String extractedAt,
      boolean truncated,
      List<DocumentTextPage> pages
  ) {}

  public record DocumentTextPage(int page, String text) {}

  public record TextExtractionResult(byte[] textJson, int textLength, boolean truncated) {}

  public record DocumentMetadata(
      int pageCount,
      String title,
      String author,
      String subject,
      String creator,
      String producer,
      Instant createdAt,
      Instant modifiedAt,
      Integer textLength,
      Boolean textTruncated
  ) {
    DocumentMetadata withTextStats(TextExtractionResult result) {
      return new DocumentMetadata(
          pageCount,
          title,
          author,
          subject,
          creator,
          producer,
          createdAt,
          modifiedAt,
          result.textLength(),
          result.truncated()
      );
    }
  }
}
