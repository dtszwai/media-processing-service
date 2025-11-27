package com.mediaservice.lambda.config;

import software.amazon.awssdk.auth.credentials.DefaultCredentialsProvider;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.s3.S3Client;

import java.net.URI;

public class AwsClientFactory {

  private static final LambdaConfig CONFIG = LambdaConfig.getInstance();

  private static DynamoDbClient dynamoDbClient;
  private static S3Client s3Client;

  public static synchronized DynamoDbClient getDynamoDbClient() {
    if (dynamoDbClient == null) {
      var builder = DynamoDbClient.builder()
          .region(Region.of(CONFIG.getAwsRegion()))
          .credentialsProvider(DefaultCredentialsProvider.create());
      if (CONFIG.getDynamoDbEndpoint() != null) {
        builder.endpointOverride(URI.create(CONFIG.getDynamoDbEndpoint()));
      }
      dynamoDbClient = builder.build();
    }
    return dynamoDbClient;
  }

  public static synchronized S3Client getS3Client() {
    if (s3Client == null) {
      var builder = S3Client.builder()
          .region(Region.of(CONFIG.getAwsRegion()))
          .credentialsProvider(DefaultCredentialsProvider.create())
          .forcePathStyle(true);
      if (CONFIG.getS3Endpoint() != null) {
        builder.endpointOverride(URI.create(CONFIG.getS3Endpoint()));
      }
      s3Client = builder.build();
    }
    return s3Client;
  }
}
