package com.mediaservice.lambda.service;

import net.coobird.thumbnailator.Thumbnails;
import net.coobird.thumbnailator.geometry.Positions;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;

public class ImageProcessingService {

  private static final Logger logger = LoggerFactory.getLogger(ImageProcessingService.class);
  private static final int DEFAULT_WIDTH = 500;

  private BufferedImage watermarkImage;

  public ImageProcessingService() {
    loadWatermark();
  }

  private void loadWatermark() {
    try (InputStream watermarkStream = ImageProcessingService.class
        .getResourceAsStream("/media-service-watermark.png")) {
      if (watermarkStream != null) {
        watermarkImage = ImageIO.read(watermarkStream);
        if (watermarkImage == null) {
          throw new IOException(
              "ImageIO returned null for watermark image. The file might be corrupted or in an unsupported format.");
        }
        logger.info("Watermark image loaded successfully");
      } else {
        throw new IOException("Watermark image resource not found in classpath at /media-service-watermark.png");
      }
    } catch (IOException e) {
      logger.error("Failed to load watermark image: {}", e.getMessage());
      throw new RuntimeException("Failed to initialize ImageProcessingService: Watermark image could not be loaded", e);
    }
  }

  public byte[] processImage(byte[] imageData, Integer targetWidth) throws IOException {
    return processImageInternal(imageData, targetWidth, Positions.BOTTOM_RIGHT, "Processing");
  }

  public byte[] resizeImage(byte[] imageData, Integer targetWidth) throws IOException {
    return processImageInternal(imageData, targetWidth, Positions.BOTTOM_LEFT, "Resizing");
  }

  private byte[] processImageInternal(byte[] imageData, Integer targetWidth, net.coobird.thumbnailator.geometry.Position watermarkPosition, String operation) throws IOException {
    int width = (targetWidth != null && targetWidth > 0) ? targetWidth : DEFAULT_WIDTH;

    logger.info("{} image with target width: {}", operation, width);

    long startTime = System.currentTimeMillis();

    ByteArrayInputStream inputStream = new ByteArrayInputStream(imageData);
    ByteArrayOutputStream outputStream = new ByteArrayOutputStream();

    try {
      var thumbnailBuilder = Thumbnails.of(inputStream)
          .width(width)
          .outputFormat("jpeg")
          .outputQuality(0.9);

      // Add watermark if available
      if (watermarkImage != null) {
        int watermarkWidth = Math.max(width / 7, 30);
        BufferedImage resizedWatermark = Thumbnails.of(watermarkImage)
            .width(watermarkWidth)
            .asBufferedImage();

        thumbnailBuilder.watermark(watermarkPosition, resizedWatermark, 1.0f);
      }

      thumbnailBuilder.toOutputStream(outputStream);

      long processingTime = System.currentTimeMillis() - startTime;
      logger.info("Image {} completed in {} ms", operation.toLowerCase(), processingTime);

      return outputStream.toByteArray();
    } catch (IOException e) {
      logger.error("Failed to {} image: {}", operation.toLowerCase(), e.getMessage());
      throw e;
    }
  }
}
