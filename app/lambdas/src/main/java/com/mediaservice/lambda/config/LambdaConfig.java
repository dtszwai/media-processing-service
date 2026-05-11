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
  private final String generationTopicArn;

  // Redis Configuration (for Analytics Lambda)
  private final String redisHost;
  private final int redisPort;

  // Image Processing Configuration
  private final int defaultWidth;
  private final int minWatermarkWidth;
  private final float watermarkWidthRatio;
  private final float jpegQuality;
  private final float webpQuality;

  // Document Processing Configuration
  private final int documentMaxPages;
  private final int documentPreviewDpi;
  private final int documentMaxTextChars;

  // Webhook Configuration
  private final String webhookSecret;

  private LambdaConfig() {
    this.awsRegion = getEnv("AWS_REGION", "us-west-2");
    this.dynamoDbEndpoint = getEnv("AWS_DYNAMODB_ENDPOINT", null);
    this.s3Endpoint = getEnv("AWS_S3_ENDPOINT", null);
    this.bucketName = getEnv("MEDIA_BUCKET_NAME", "media-bucket");
    this.tableName = getEnv("MEDIA_DYNAMODB_TABLE_NAME", "media");
    this.generationTopicArn = getEnv("MEDIA_GENERATION_TOPIC_ARN", null);

    this.redisHost = getEnv("REDIS_HOST", "localhost");
    this.redisPort = getEnvInt("REDIS_PORT", 6379);

    this.defaultWidth = getEnvInt("IMAGE_DEFAULT_WIDTH", 500);
    this.minWatermarkWidth = getEnvInt("IMAGE_MIN_WATERMARK_WIDTH", 30);
    this.watermarkWidthRatio = getEnvFloat("IMAGE_WATERMARK_WIDTH_RATIO", 1.0f / 7.0f);
    this.jpegQuality = getEnvFloat("IMAGE_JPEG_QUALITY", 0.9f);
    this.webpQuality = getEnvFloat("IMAGE_WEBP_QUALITY", 0.85f);

    this.documentMaxPages = getEnvInt("DOCUMENT_MAX_PAGES", 200);
    this.documentPreviewDpi = getEnvInt("DOCUMENT_PREVIEW_DPI", 120);
    this.documentMaxTextChars = getEnvInt("DOCUMENT_MAX_TEXT_CHARS", 2_000_000);

    this.webhookSecret = getEnv("WEBHOOK_SECRET", null);
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
