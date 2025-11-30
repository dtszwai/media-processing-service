package com.mediaservice.lambda.config;

import lombok.Getter;

@Getter
public final class LambdaConfig {

  private static final LambdaConfig INSTANCE = new LambdaConfig();

  // AWS Configuration
  private final String awsRegion;
  private final String dynamoDbEndpoint;
  private final String s3Endpoint;
  private final String bucketName;
  private final String tableName;

  // Image Processing Configuration
  private final int defaultWidth;
  private final int minWatermarkWidth;
  private final float watermarkWidthRatio;
  private final float jpegQuality;
  private final float webpQuality;

  private LambdaConfig() {
    this.awsRegion = getEnv("AWS_REGION", "us-west-2");
    this.dynamoDbEndpoint = getEnv("AWS_DYNAMODB_ENDPOINT", null);
    this.s3Endpoint = getEnv("AWS_S3_ENDPOINT", null);
    this.bucketName = getEnv("MEDIA_BUCKET_NAME", "media-bucket");
    this.tableName = getEnv("MEDIA_DYNAMODB_TABLE_NAME", "media");

    this.defaultWidth = getEnvInt("IMAGE_DEFAULT_WIDTH", 500);
    this.minWatermarkWidth = getEnvInt("IMAGE_MIN_WATERMARK_WIDTH", 30);
    this.watermarkWidthRatio = getEnvFloat("IMAGE_WATERMARK_WIDTH_RATIO", 1.0f / 7.0f);
    this.jpegQuality = getEnvFloat("IMAGE_JPEG_QUALITY", 0.9f);
    this.webpQuality = getEnvFloat("IMAGE_WEBP_QUALITY", 0.85f);
  }

  public static LambdaConfig getInstance() {
    return INSTANCE;
  }

  private static String getEnv(String key, String defaultValue) {
    String value = System.getenv(key);
    return (value != null && !value.isEmpty()) ? value : defaultValue;
  }

  private static int getEnvInt(String key, int defaultValue) {
    String value = System.getenv(key);
    if (value == null || value.isEmpty()) {
      return defaultValue;
    }
    try {
      return Integer.parseInt(value);
    } catch (NumberFormatException e) {
      return defaultValue;
    }
  }

  private static float getEnvFloat(String key, float defaultValue) {
    String value = System.getenv(key);
    if (value == null || value.isEmpty()) {
      return defaultValue;
    }
    try {
      return Float.parseFloat(value);
    } catch (NumberFormatException e) {
      return defaultValue;
    }
  }
}
