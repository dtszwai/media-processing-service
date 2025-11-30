package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.common.model.OutputFormat;
import net.coobird.thumbnailator.Thumbnails;
import net.coobird.thumbnailator.geometry.Position;
import net.coobird.thumbnailator.geometry.Positions;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.imageio.ImageIO;
import javax.imageio.ImageWriter;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.util.Iterator;

public class ImageProcessingService {
  private static final Logger logger = LoggerFactory.getLogger(ImageProcessingService.class);

  private final LambdaConfig config;
  private final BufferedImage watermarkImage;

  static {
    // Ensure ImageIO plugins are scanned (needed for webp-imageio in Lambda environment)
    ImageIO.scanForPlugins();
    logAvailableFormats();
  }

  private static void logAvailableFormats() {
    String[] writerFormats = ImageIO.getWriterFormatNames();
    logger.info("Available ImageIO writer formats: {}", String.join(", ", writerFormats));
  }

  public ImageProcessingService() {
    this.config = LambdaConfig.getInstance();
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

  public byte[] processImage(byte[] imageData, Integer targetWidth, OutputFormat outputFormat) throws IOException {
    return processImageInternal(imageData, targetWidth, Positions.BOTTOM_RIGHT, outputFormat);
  }

  public byte[] resizeImage(byte[] imageData, Integer targetWidth, OutputFormat outputFormat) throws IOException {
    return processImageInternal(imageData, targetWidth, Positions.BOTTOM_LEFT, outputFormat);
  }

  private byte[] processImageInternal(byte[] imageData, Integer targetWidth, Position watermarkPosition,
      OutputFormat outputFormat) throws IOException {
    int width = (targetWidth != null && targetWidth > 0) ? targetWidth : config.getDefaultWidth();
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;

    // Check if the format is supported
    if (!isFormatSupported(format.getFormat())) {
      logger.warn("Output format '{}' is not supported, falling back to JPEG", format.getFormat());
      format = OutputFormat.JPEG;
    }

    logger.info("Processing image with format: {}, width: {}", format.getFormat(), width);

    var inputStream = new ByteArrayInputStream(imageData);
    var outputStream = new ByteArrayOutputStream();
    int watermarkWidth = Math.max(
        (int) (width * config.getWatermarkWidthRatio()),
        config.getMinWatermarkWidth());
    var resizedWatermark = Thumbnails.of(watermarkImage).width(watermarkWidth).asBufferedImage();

    var builder = Thumbnails.of(inputStream)
        .width(width)
        .outputFormat(format.getFormat())
        .watermark(watermarkPosition, resizedWatermark, 1.0f);

    // Set quality for formats that support it
    if (format == OutputFormat.JPEG) {
      builder.outputQuality(config.getJpegQuality());
    } else if (format == OutputFormat.WEBP) {
      builder.outputQuality(config.getWebpQuality());
    }

    builder.toOutputStream(outputStream);
    logger.info("Image processed successfully, output size: {} bytes", outputStream.size());
    return outputStream.toByteArray();
  }

  private boolean isFormatSupported(String formatName) {
    Iterator<ImageWriter> writers = ImageIO.getImageWritersByFormatName(formatName);
    return writers.hasNext();
  }
}
