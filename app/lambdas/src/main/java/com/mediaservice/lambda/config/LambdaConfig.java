package com.mediaservice.lambda.config;

public final class LambdaConfig {

  private static final LambdaConfig INSTANCE = new LambdaConfig();

  private final String awsRegion;
  private final String dynamoDbEndpoint;
  private final String s3Endpoint;
  private final String bucketName;
  private final String tableName;

  private LambdaConfig() {
    this.awsRegion = getEnv("AWS_REGION", "us-west-2");
    this.dynamoDbEndpoint = getEnv("AWS_DYNAMODB_ENDPOINT", null);
    this.s3Endpoint = getEnv("AWS_S3_ENDPOINT", null);
    this.bucketName = getEnv("MEDIA_BUCKET_NAME", "media-bucket");
    this.tableName = getEnv("MEDIA_DYNAMODB_TABLE_NAME", "media");
  }

  public static LambdaConfig getInstance() {
    return INSTANCE;
  }

  private static String getEnv(String key, String defaultValue) {
    String value = System.getenv(key);
    return (value != null && !value.isEmpty()) ? value : defaultValue;
  }

  public String getAwsRegion() {
    return awsRegion;
  }

  public String getDynamoDbEndpoint() {
    return dynamoDbEndpoint;
  }

  public String getS3Endpoint() {
    return s3Endpoint;
  }

  public String getBucketName() {
    return bucketName;
  }

  public String getTableName() {
    return tableName;
  }
}
