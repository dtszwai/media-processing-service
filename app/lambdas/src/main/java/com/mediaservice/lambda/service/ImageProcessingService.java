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
  private static final String WATERMARK_RESOURCE_PATH = "/media-service-watermark.png";

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
    this.watermarkImage = WatermarkLoader.load(WATERMARK_RESOURCE_PATH, ImageProcessingService.class);
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
    OutputFormat format = resolveOutputFormat(outputFormat);

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

  /**
   * Resolves the output format, defaulting to JPEG if null or unsupported.
   */
  private OutputFormat resolveOutputFormat(OutputFormat requested) {
    OutputFormat format = (requested != null) ? requested : OutputFormat.JPEG;
    if (!isFormatSupported(format.getFormat())) {
      logger.warn("Output format '{}' is not supported, falling back to JPEG", format.getFormat());
      return OutputFormat.JPEG;
    }
    return format;
  }

  private static final int PREVIEW_MAX_WIDTH = 800;
  private static final float PREVIEW_QUALITY = 0.6f;

  /**
   * Generate a preview image with heavy watermark for CDN distribution.
   * Preview is lower quality and smaller for efficient CDN caching.
   *
   * @param imageData Original image bytes
   * @param outputFormat Desired output format (defaults to JPEG if null/unsupported)
   * @return Preview image bytes
   * @throws IOException If image processing fails
   */
  public byte[] generatePreview(byte[] imageData, OutputFormat outputFormat) throws IOException {
    OutputFormat format = resolveOutputFormat(outputFormat);

    logger.info("Generating preview with format: {}", format.getFormat());

    var inputStream = new ByteArrayInputStream(imageData);
    var outputStream = new ByteArrayOutputStream();

    // Heavier watermark for preview - center position, more visible
    int watermarkWidth = Math.max(200, config.getMinWatermarkWidth() * 2);
    var resizedWatermark = Thumbnails.of(watermarkImage).width(watermarkWidth).asBufferedImage();

    var builder = Thumbnails.of(inputStream)
        .width(PREVIEW_MAX_WIDTH)
        .outputFormat(format.getFormat())
        .watermark(Positions.CENTER, resizedWatermark, 0.5f)  // Center, more visible
        .outputQuality(PREVIEW_QUALITY);

    builder.toOutputStream(outputStream);
    logger.info("Preview generated, size: {} bytes", outputStream.size());
    return outputStream.toByteArray();
  }
}
