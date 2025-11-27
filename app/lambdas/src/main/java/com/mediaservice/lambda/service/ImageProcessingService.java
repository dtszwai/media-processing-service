package com.mediaservice.lambda.service;

import net.coobird.thumbnailator.Thumbnails;
import net.coobird.thumbnailator.geometry.Position;
import net.coobird.thumbnailator.geometry.Positions;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;

public class ImageProcessingService {
  private static final int DEFAULT_WIDTH = 500;
  private final BufferedImage watermarkImage;

  public ImageProcessingService() {
    this.watermarkImage = loadWatermark();
  }

  private BufferedImage loadWatermark() {
    try (var watermarkStream = ImageProcessingService.class.getResourceAsStream("/media-service-watermark.png")) {
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

  public byte[] processImage(byte[] imageData, Integer targetWidth) throws IOException {
    return processImageInternal(imageData, targetWidth, Positions.BOTTOM_RIGHT);
  }

  public byte[] resizeImage(byte[] imageData, Integer targetWidth) throws IOException {
    return processImageInternal(imageData, targetWidth, Positions.BOTTOM_LEFT);
  }

  private byte[] processImageInternal(byte[] imageData, Integer targetWidth, Position watermarkPosition)
      throws IOException {
    int width = (targetWidth != null && targetWidth > 0) ? targetWidth : DEFAULT_WIDTH;
    var inputStream = new ByteArrayInputStream(imageData);
    var outputStream = new ByteArrayOutputStream();
    int watermarkWidth = Math.max(width / 7, 30);
    var resizedWatermark = Thumbnails.of(watermarkImage).width(watermarkWidth).asBufferedImage();
    Thumbnails.of(inputStream)
        .width(width)
        .outputFormat("jpeg")
        .outputQuality(0.9)
        .watermark(watermarkPosition, resizedWatermark, 1.0f)
        .toOutputStream(outputStream);
    return outputStream.toByteArray();
  }
}
