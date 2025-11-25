package com.mediaservice.lambda.config;

import software.amazon.awssdk.auth.credentials.DefaultCredentialsProvider;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.s3.S3Client;

import java.net.URI;

public class AwsClientFactory {

  private static final String AWS_REGION = System.getenv("AWS_REGION") != null
      ? System.getenv("AWS_REGION")
      : "us-west-2";

  private static final String DYNAMODB_ENDPOINT = System.getenv("AWS_DYNAMODB_ENDPOINT");
  private static final String S3_ENDPOINT = System.getenv("AWS_S3_ENDPOINT");

  private static DynamoDbClient dynamoDbClient;
  private static S3Client s3Client;

  public static synchronized DynamoDbClient getDynamoDbClient() {
    if (dynamoDbClient == null) {
      var builder = DynamoDbClient.builder()
          .region(Region.of(AWS_REGION))
          .credentialsProvider(DefaultCredentialsProvider.create());

      if (DYNAMODB_ENDPOINT != null && !DYNAMODB_ENDPOINT.isEmpty()) {
        builder.endpointOverride(URI.create(DYNAMODB_ENDPOINT));
      }

      dynamoDbClient = builder.build();
    }
    return dynamoDbClient;
  }

  public static synchronized S3Client getS3Client() {
    if (s3Client == null) {
      var builder = S3Client.builder()
          .region(Region.of(AWS_REGION))
          .credentialsProvider(DefaultCredentialsProvider.create())
          .forcePathStyle(true);

      if (S3_ENDPOINT != null && !S3_ENDPOINT.isEmpty()) {
        builder.endpointOverride(URI.create(S3_ENDPOINT));
      }

      s3Client = builder.build();
    }
    return s3Client;
  }
}
