package com.mediaservice.integration;

import org.testcontainers.containers.localstack.LocalStackContainer;
import org.testcontainers.utility.DockerImageName;
import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.CreateBucketRequest;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sns.model.CreateTopicRequest;

import static org.testcontainers.containers.localstack.LocalStackContainer.Service.*;

/**
 * Shared LocalStack container for integration tests.
 * Uses a singleton pattern to ensure only one container is started.
 */
public class LocalStackTestConfig {
  private static final LocalStackContainer localStack;

  static {
    localStack = new LocalStackContainer(DockerImageName.parse("localstack/localstack:3.4"))
        .withServices(S3, DYNAMODB, SNS);
    localStack.start();
    initializeResources();
  }

  public static LocalStackContainer getLocalStack() {
    return localStack;
  }

  public static String getEndpoint() {
    return localStack.getEndpointOverride(DYNAMODB).toString();
  }

  public static String getS3Endpoint() {
    return localStack.getEndpointOverride(S3).toString();
  }

  public static String getSnsEndpoint() {
    return localStack.getEndpointOverride(SNS).toString();
  }

  public static String getAccessKey() {
    return localStack.getAccessKey();
  }

  public static String getSecretKey() {
    return localStack.getSecretKey();
  }

  public static String getRegion() {
    return localStack.getRegion();
  }

  private static void initializeResources() {
    var credentials = StaticCredentialsProvider.create(
        AwsBasicCredentials.create(localStack.getAccessKey(), localStack.getSecretKey()));
    var region = Region.of(localStack.getRegion());

    try (var dynamoClient = DynamoDbClient.builder()
        .endpointOverride(localStack.getEndpointOverride(DYNAMODB))
        .credentialsProvider(credentials)
        .region(region)
        .build();
        var s3Client = S3Client.builder()
            .endpointOverride(localStack.getEndpointOverride(S3))
            .credentialsProvider(credentials)
            .region(region)
            .forcePathStyle(true)
            .build();
        var snsClient = SnsClient.builder()
            .endpointOverride(localStack.getEndpointOverride(SNS))
            .credentialsProvider(credentials)
            .region(region)
            .build()) {
      dynamoClient.createTable(CreateTableRequest.builder()
          .tableName("media")
          .keySchema(
              KeySchemaElement.builder().attributeName("PK").keyType(KeyType.HASH).build(),
              KeySchemaElement.builder().attributeName("SK").keyType(KeyType.RANGE).build())
          .attributeDefinitions(
              AttributeDefinition.builder().attributeName("PK").attributeType(ScalarAttributeType.S).build(),
              AttributeDefinition.builder().attributeName("SK").attributeType(ScalarAttributeType.S).build())
          .billingMode(BillingMode.PAY_PER_REQUEST)
          .build());
      s3Client.createBucket(CreateBucketRequest.builder().bucket("media-bucket").build());
      snsClient.createTopic(CreateTopicRequest.builder().name("media-management-topic").build());
    } catch (Exception e) {
      throw new RuntimeException("Failed to initialize LocalStack resources", e);
    }
  }
}
