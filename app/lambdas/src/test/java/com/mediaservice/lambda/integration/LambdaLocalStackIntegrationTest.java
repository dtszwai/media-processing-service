package com.mediaservice.lambda.integration;

import com.mediaservice.common.model.MediaStatus;
import org.junit.jupiter.api.*;
import org.testcontainers.containers.localstack.LocalStackContainer;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;
import org.testcontainers.utility.DockerImageName;
import software.amazon.awssdk.auth.credentials.AwsBasicCredentials;
import software.amazon.awssdk.auth.credentials.StaticCredentialsProvider;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.*;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.CreateBucketRequest;
import software.amazon.awssdk.services.s3.model.GetObjectRequest;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.time.Instant;
import java.util.Map;

import static org.assertj.core.api.Assertions.assertThat;
import static org.testcontainers.containers.localstack.LocalStackContainer.Service.DYNAMODB;
import static org.testcontainers.containers.localstack.LocalStackContainer.Service.S3;

/**
 * Integration tests for Lambda services using LocalStack.
 * Tests DynamoDB and S3 operations in isolation.
 * <p>
 * The container lifecycle is managed by the Testcontainers JUnit 5 extension
 * via the {@code @Container} annotation. The
 * {@code @SuppressWarnings("resource")}
 * is required because static analysis tools cannot track the extension's
 * automatic cleanup.
 *
 * @see <a href=
 *      "https://java.testcontainers.org/test_framework_integration/junit_5/">Testcontainers
 *      JUnit 5</a>
 */
@Testcontainers
class LambdaLocalStackIntegrationTest {
  @Container
  @SuppressWarnings("resource") // Lifecycle managed by Testcontainers JUnit 5 extension
  static LocalStackContainer localStack = new LocalStackContainer(DockerImageName.parse("localstack/localstack:3.4"))
      .withServices(S3, DYNAMODB)
      .withReuse(true);

  private static DynamoDbClient dynamoDbClient;
  private static S3Client s3Client;

  private static final String TABLE_NAME = "media";
  private static final String BUCKET_NAME = "media-bucket";

  @BeforeAll
  static void setupClients() {
    var credentials = StaticCredentialsProvider
        .create(AwsBasicCredentials.create(localStack.getAccessKey(), localStack.getSecretKey()));
    dynamoDbClient = DynamoDbClient.builder()
        .endpointOverride(localStack.getEndpointOverride(DYNAMODB))
        .credentialsProvider(credentials)
        .region(Region.US_WEST_2)
        .build();
    s3Client = S3Client.builder()
        .endpointOverride(localStack.getEndpointOverride(S3))
        .credentialsProvider(credentials)
        .region(Region.US_WEST_2)
        .forcePathStyle(true)
        .build();
    dynamoDbClient.createTable(CreateTableRequest.builder()
        .tableName(TABLE_NAME)
        .keySchema(
            KeySchemaElement.builder().attributeName("PK").keyType(KeyType.HASH).build(),
            KeySchemaElement.builder().attributeName("SK").keyType(KeyType.RANGE).build())
        .attributeDefinitions(
            AttributeDefinition.builder().attributeName("PK").attributeType(ScalarAttributeType.S).build(),
            AttributeDefinition.builder().attributeName("SK").attributeType(ScalarAttributeType.S).build())
        .billingMode(BillingMode.PAY_PER_REQUEST)
        .build());
    s3Client.createBucket(CreateBucketRequest.builder().bucket(BUCKET_NAME).build());
  }

  @AfterAll
  static void closeClients() {
    if (dynamoDbClient != null) {
      dynamoDbClient.close();
    }
    if (s3Client != null) {
      s3Client.close();
    }
  }

  @BeforeEach
  void cleanUp() {
    // Clean DynamoDB
    var scanResult = dynamoDbClient.scan(ScanRequest.builder().tableName(TABLE_NAME).build());
    for (var item : scanResult.items()) {
      dynamoDbClient.deleteItem(DeleteItemRequest.builder()
          .tableName(TABLE_NAME)
          .key(Map.of("PK", item.get("PK"), "SK", item.get("SK")))
          .build());
    }
    // Clean S3
    var objects = s3Client.listObjectsV2(b -> b.bucket(BUCKET_NAME));
    for (var obj : objects.contents()) {
      s3Client.deleteObject(b -> b.bucket(BUCKET_NAME).key(obj.key()));
    }
  }

  @Nested
  @DisplayName("DynamoDB Operations")
  class DynamoDbOperations {
    @Test
    @DisplayName("should store and retrieve media metadata")
    void shouldStoreAndRetrieveMedia() {
      var mediaId = "test-media-123";
      createMediaRecord(mediaId, "test.jpg", MediaStatus.PENDING);
      var result = getMediaRecord(mediaId);
      assertThat(result).isNotNull();
      assertThat(result.get("name").s()).isEqualTo("test.jpg");
      assertThat(result.get("status").s()).isEqualTo("PENDING");
    }

    @Test
    @DisplayName("should update status conditionally")
    void shouldUpdateStatusConditionally() {
      var mediaId = "test-media-123";
      createMediaRecord(mediaId, "test.jpg", MediaStatus.PENDING);
      // Update PENDING -> PROCESSING
      boolean updated = updateStatusConditionally(mediaId, MediaStatus.PROCESSING, MediaStatus.PENDING);
      assertThat(updated).isTrue();
      var result = getMediaRecord(mediaId);
      assertThat(result.get("status").s()).isEqualTo("PROCESSING");
    }

    @Test
    @DisplayName("should fail conditional update when status mismatch")
    void shouldFailConditionalUpdateOnMismatch() {
      var mediaId = "test-media-123";
      createMediaRecord(mediaId, "test.jpg", MediaStatus.PROCESSING);
      // Try to update expecting PENDING but actual is PROCESSING
      boolean updated = updateStatusConditionally(mediaId, MediaStatus.COMPLETE, MediaStatus.PENDING);
      assertThat(updated).isFalse();
      var result = getMediaRecord(mediaId);
      assertThat(result.get("status").s()).isEqualTo("PROCESSING");
    }

    @Test
    @DisplayName("should delete media and return old values")
    void shouldDeleteMedia() {
      var mediaId = "test-media-123";
      createMediaRecord(mediaId, "test.jpg", MediaStatus.COMPLETE);
      var deleted = dynamoDbClient.deleteItem(DeleteItemRequest.builder()
          .tableName(TABLE_NAME)
          .key(keyFor(mediaId))
          .returnValues(ReturnValue.ALL_OLD)
          .build());
      assertThat(deleted.attributes()).isNotEmpty();
      assertThat(deleted.attributes().get("name").s()).isEqualTo("test.jpg");
      var result = getMediaRecord(mediaId);
      assertThat(result).isEmpty();
    }
  }

  @Nested
  @DisplayName("S3 Operations")
  class S3Operations {
    @Test
    @DisplayName("should upload and retrieve file")
    void shouldUploadAndRetrieveFile() throws Exception {
      var mediaId = "test-media-123";
      byte[] imageData = createTestImage();
      s3Client.putObject(
          PutObjectRequest.builder()
              .bucket(BUCKET_NAME)
              .key("uploads/" + mediaId + "/test.jpg")
              .contentType("image/jpeg")
              .build(),
          RequestBody.fromBytes(imageData));
      var retrieved = s3Client.getObjectAsBytes(GetObjectRequest.builder()
          .bucket(BUCKET_NAME)
          .key("uploads/" + mediaId + "/test.jpg")
          .build());
      assertThat(retrieved.asByteArray()).isEqualTo(imageData);
    }

    @Test
    @DisplayName("should handle upload to resized folder")
    void shouldUploadToResizedFolder() throws Exception {
      var mediaId = "test-media-123";
      byte[] processedData = createTestImage();
      s3Client.putObject(
          PutObjectRequest.builder()
              .bucket(BUCKET_NAME)
              .key("resized/" + mediaId + "/test.jpg")
              .contentType("image/jpeg")
              .build(),
          RequestBody.fromBytes(processedData));
      var objects = s3Client.listObjectsV2(b -> b
          .bucket(BUCKET_NAME)
          .prefix("resized/" + mediaId + "/"));
      assertThat(objects.contents()).hasSize(1);
      assertThat(objects.contents().get(0).key()).isEqualTo("resized/" + mediaId + "/test.jpg");
    }

    @Test
    @DisplayName("should delete files from both folders")
    void shouldDeleteFiles() throws Exception {
      var mediaId = "test-media-123";
      byte[] data = createTestImage();
      // Upload to both folders
      s3Client.putObject(
          PutObjectRequest.builder()
              .bucket(BUCKET_NAME)
              .key("uploads/" + mediaId + "/test.jpg")
              .build(),
          RequestBody.fromBytes(data));

      s3Client.putObject(
          PutObjectRequest.builder()
              .bucket(BUCKET_NAME)
              .key("resized/" + mediaId + "/test.jpg")
              .build(),
          RequestBody.fromBytes(data));
      // Delete both
      s3Client.deleteObject(b -> b.bucket(BUCKET_NAME).key("uploads/" + mediaId + "/test.jpg"));
      s3Client.deleteObject(b -> b.bucket(BUCKET_NAME).key("resized/" + mediaId + "/test.jpg"));
      var remaining = s3Client.listObjectsV2(b -> b.bucket(BUCKET_NAME));
      assertThat(remaining.contents()).isEmpty();
    }
  }

  @Nested
  @DisplayName("Full Processing Flow Simulation")
  class ProcessingFlowSimulation {
    @Test
    @DisplayName("should simulate complete processing workflow")
    void shouldSimulateProcessingWorkflow() throws Exception {
      var mediaId = "workflow-test-123";
      byte[] originalImage = createTestImage();

      // Step 1: API creates record with PENDING status and uploads original
      createMediaRecord(mediaId, "workflow.jpg", MediaStatus.PENDING);
      s3Client.putObject(
          PutObjectRequest.builder()
              .bucket(BUCKET_NAME)
              .key("uploads/" + mediaId + "/workflow.jpg")
              .build(),
          RequestBody.fromBytes(originalImage));

      // Step 2: Lambda updates status to PROCESSING
      boolean updated = updateStatusConditionally(mediaId, MediaStatus.PROCESSING, MediaStatus.PENDING);
      assertThat(updated).isTrue();

      // Step 3: Lambda retrieves original, processes, and uploads processed
      var retrievedData = s3Client.getObjectAsBytes(GetObjectRequest.builder()
          .bucket(BUCKET_NAME)
          .key("uploads/" + mediaId + "/workflow.jpg")
          .build());

      // Simulate processing (in real code, this would resize/watermark)
      byte[] processedImage = retrievedData.asByteArray();

      s3Client.putObject(
          PutObjectRequest.builder()
              .bucket(BUCKET_NAME)
              .key("resized/" + mediaId + "/workflow.jpg")
              .build(),
          RequestBody.fromBytes(processedImage));

      // Step 4: Lambda updates status to COMPLETE
      updated = updateStatusConditionally(mediaId, MediaStatus.COMPLETE, MediaStatus.PROCESSING);
      assertThat(updated).isTrue();

      // Verify final state
      var finalRecord = getMediaRecord(mediaId);
      assertThat(finalRecord.get("status").s()).isEqualTo("COMPLETE");

      var resizedExists = s3Client.listObjectsV2(b -> b
          .bucket(BUCKET_NAME)
          .prefix("resized/" + mediaId + "/"));
      assertThat(resizedExists.contents()).hasSize(1);
    }
  }

  // Helper methods

  private void createMediaRecord(String mediaId, String name, MediaStatus status) {
    var now = Instant.now().toString();
    dynamoDbClient.putItem(PutItemRequest.builder()
        .tableName(TABLE_NAME)
        .item(Map.of(
            "PK", s("MEDIA#" + mediaId),
            "SK", s("METADATA"),
            "name", s(name),
            "size", n("1024"),
            "mimetype", s("image/jpeg"),
            "status", s(status.name()),
            "width", n("500"),
            "createdAt", s(now),
            "updatedAt", s(now)))
        .build());
  }

  private Map<String, AttributeValue> getMediaRecord(String mediaId) {
    var response = dynamoDbClient.getItem(GetItemRequest.builder()
        .tableName(TABLE_NAME)
        .key(keyFor(mediaId))
        .build());
    return response.item();
  }

  private boolean updateStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus) {
    try {
      dynamoDbClient.updateItem(UpdateItemRequest.builder()
          .tableName(TABLE_NAME)
          .key(keyFor(mediaId))
          .updateExpression("SET #status = :newStatus, updatedAt = :updatedAt")
          .conditionExpression("#status = :expectedStatus")
          .expressionAttributeNames(Map.of("#status", "status"))
          .expressionAttributeValues(Map.of(
              ":newStatus", s(newStatus.name()),
              ":expectedStatus", s(expectedStatus.name()),
              ":updatedAt", s(Instant.now().toString())))
          .build());
      return true;
    } catch (ConditionalCheckFailedException e) {
      return false;
    }
  }

  private Map<String, AttributeValue> keyFor(String mediaId) {
    return Map.of("PK", s("MEDIA#" + mediaId), "SK", s("METADATA"));
  }

  private AttributeValue s(String value) {
    return AttributeValue.builder().s(value).build();
  }

  private AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }

  private byte[] createTestImage() throws Exception {
    var image = new BufferedImage(100, 100, BufferedImage.TYPE_INT_RGB);
    var baos = new ByteArrayOutputStream();
    ImageIO.write(image, "jpg", baos);
    return baos.toByteArray();
  }
}
