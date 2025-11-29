package com.mediaservice.integration;

import org.junit.jupiter.api.BeforeEach;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ScanRequest;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.Delete;
import software.amazon.awssdk.services.s3.model.DeleteObjectsRequest;
import software.amazon.awssdk.services.s3.model.ListObjectsV2Request;
import software.amazon.awssdk.services.s3.model.ObjectIdentifier;

import java.util.Map;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles("test")
public abstract class BaseIntegrationTest {

  @Autowired
  protected DynamoDbClient dynamoDbClient;

  @Autowired
  protected S3Client s3Client;

  @DynamicPropertySource
  static void configureProperties(DynamicPropertyRegistry registry) {
    // Ensure LocalStack is started
    LocalStackTestConfig.getLocalStack();

    // Configure AWS endpoints to use LocalStack
    registry.add("aws.dynamodb.endpoint", LocalStackTestConfig::getEndpoint);
    registry.add("aws.s3.endpoint", LocalStackTestConfig::getS3Endpoint);
    registry.add("aws.s3.presigner-endpoint", LocalStackTestConfig::getS3Endpoint);
    registry.add("aws.sns.endpoint", LocalStackTestConfig::getSnsEndpoint);
    registry.add("aws.dynamodb.table-name", () -> "media");
    registry.add("aws.s3.bucket-name", () -> "media-bucket");
    registry.add("aws.sns.topic-arn",
        () -> "arn:aws:sns:" + LocalStackTestConfig.getRegion() + ":000000000000:media-management-topic");

    // Configure AWS credentials
    registry.add("aws.region", LocalStackTestConfig::getRegion);

    // Set system properties for AWS SDK
    System.setProperty("aws.accessKeyId", LocalStackTestConfig.getAccessKey());
    System.setProperty("aws.secretAccessKey", LocalStackTestConfig.getSecretKey());
  }

  @BeforeEach
  void cleanUp() {
    cleanDynamoDbTable();
    cleanS3Bucket();
  }

  private void cleanDynamoDbTable() {
    try {
      var scanResult = dynamoDbClient.scan(ScanRequest.builder()
          .tableName("media")
          .build());

      for (var item : scanResult.items()) {
        dynamoDbClient.deleteItem(b -> b
            .tableName("media")
            .key(Map.of("PK", item.get("PK"), "SK", item.get("SK"))));
      }
    } catch (Exception e) {
      // Table might not have items yet
    }
  }

  private void cleanS3Bucket() {
    try {
      var objects = s3Client.listObjectsV2(ListObjectsV2Request.builder()
          .bucket("media-bucket")
          .build());

      if (!objects.contents().isEmpty()) {
        var toDelete = objects.contents().stream()
            .map(o -> ObjectIdentifier.builder().key(o.key()).build())
            .toList();

        s3Client.deleteObjects(DeleteObjectsRequest.builder()
            .bucket("media-bucket")
            .delete(Delete.builder().objects(toDelete).build())
            .build());
      }
    } catch (Exception e) {
      // Bucket might be empty
    }
  }

  protected AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  protected AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }
}
