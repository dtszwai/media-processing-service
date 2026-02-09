package com.mediaservice.lambda.service;

import java.awt.image.BufferedImage;
import java.io.IOException;
import javax.imageio.ImageIO;

final class WatermarkLoader {
  private WatermarkLoader() {
  }

  static BufferedImage load(String resourcePath, Class<?> resourceClass) {
    try (var watermarkStream = resourceClass.getResourceAsStream(resourcePath)) {
      if (watermarkStream == null) {
        throw new IllegalStateException("Watermark image not found at " + resourcePath);
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
}
