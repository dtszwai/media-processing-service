package com.mediaservice.shared.config;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.auth.credentials.AwsCredentialsProvider;
import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.S3Configuration;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;
import software.amazon.awssdk.services.sns.SnsClient;

import java.net.URI;

@Configuration
public class AwsConfig {
  @Value("${aws.region}")
  private String awsRegion;

  @Value("${aws.dynamodb.endpoint:}")
  private String dynamoDbEndpoint;

  @Value("${aws.s3.endpoint:}")
  private String s3Endpoint;

  @Value("${aws.sns.endpoint:}")
  private String snsEndpoint;

  @Value("${AWS_ACCESS_KEY_ID:test}")
  private String accessKeyId;

  @Value("${AWS_SECRET_ACCESS_KEY:test}")
  private String secretAccessKey;

  private AwsCredentialsProvider credentialsProvider() {
    return StaticCredentialsProvider.create(
        AwsBasicCredentials.create(accessKeyId, secretAccessKey));
  }

  @Bean
  public DynamoDbClient dynamoDbClient() {
    var builder = DynamoDbClient.builder()
        .region(Region.of(awsRegion))
        .credentialsProvider(credentialsProvider());
    if (dynamoDbEndpoint != null && !dynamoDbEndpoint.isEmpty()) {
      builder.endpointOverride(URI.create(dynamoDbEndpoint));
    }
    return builder.build();
  }

  @Bean
  public S3Client s3Client() {
    var builder = S3Client.builder()
        .region(Region.of(awsRegion))
        .credentialsProvider(credentialsProvider())
        .forcePathStyle(true);
    if (s3Endpoint != null && !s3Endpoint.isEmpty()) {
      builder.endpointOverride(URI.create(s3Endpoint));
    }
    return builder.build();
  }

  @Value("${aws.s3.presigner-endpoint:}")
  private String s3PresignerEndpoint;

  @Bean
  public S3Presigner s3Presigner() {
    var builder = S3Presigner.builder()
        .region(Region.of(awsRegion))
        .credentialsProvider(credentialsProvider())
        .serviceConfiguration(S3Configuration.builder()
            .pathStyleAccessEnabled(true)
            .build());
    // Use presigner endpoint (external URL) if set, otherwise use s3Endpoint
    String endpoint = (s3PresignerEndpoint != null && !s3PresignerEndpoint.isEmpty())
        ? s3PresignerEndpoint
        : s3Endpoint;
    if (endpoint != null && !endpoint.isEmpty()) {
      builder.endpointOverride(URI.create(endpoint));
    }
    return builder.build();
  }

  @Bean
  public SnsClient snsClient() {
    var builder = SnsClient.builder()
        .region(Region.of(awsRegion))
        .credentialsProvider(credentialsProvider());
    if (snsEndpoint != null && !snsEndpoint.isEmpty()) {
      builder.endpointOverride(URI.create(snsEndpoint));
    }
    return builder.build();
  }
}
