package com.mediaservice.shared.storage;

import lombok.extern.slf4j.Slf4j;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.DeleteObjectRequest;
import software.amazon.awssdk.services.s3.model.GetObjectRequest;
import software.amazon.awssdk.services.s3.model.HeadObjectRequest;
import software.amazon.awssdk.services.s3.model.HeadObjectResponse;
import software.amazon.awssdk.services.s3.model.NoSuchKeyException;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;
import software.amazon.awssdk.services.s3.presigner.model.GetObjectPresignRequest;
import software.amazon.awssdk.services.s3.presigner.model.PutObjectPresignRequest;

import java.io.InputStream;
import java.time.Duration;
import java.util.Optional;

/**
 * Abstract base class for S3 storage repositories providing common operations.
 *
 * <p>
 * This class provides common S3 operations that can be reused across different
 * domains. Subclasses can add domain-specific logic such as key formatting,
 * metadata handling, or content validation.
 *
 * <p>
 * Usage example:
 *
 * <pre>
 * public class MediaFileStorage extends AbstractS3StorageRepository {
 *   private static final String PREFIX = "uploads";
 *
 *   public MediaFileStorage(S3Client s3Client, S3Presigner presigner, String bucket) {
 *     super(s3Client, presigner, bucket);
 *   }
 *
 *   public void uploadMedia(String mediaId, String fileName, InputStream content, long size) {
 *     String key = buildKey(PREFIX, mediaId, fileName);
 *     upload(key, content, "image/jpeg", size);
 *   }
 * }
 * </pre>
 */
@Slf4j
public abstract class AbstractS3StorageRepository implements S3StorageRepository {
  protected final S3Client s3Client;
  protected final S3Presigner s3Presigner;
  protected final String bucketName;

  protected AbstractS3StorageRepository(S3Client s3Client, S3Presigner s3Presigner, String bucketName) {
    this.s3Client = s3Client;
    this.s3Presigner = s3Presigner;
    this.bucketName = bucketName;
  }

  // ==================== Core Operations ====================

  @Override
  public void upload(String key, InputStream inputStream, String contentType, long contentLength) {
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(contentType)
        .contentLength(contentLength)
        .build();
    s3Client.putObject(request, RequestBody.fromInputStream(inputStream, contentLength));
    log.debug("Uploaded object to S3: bucket={}, key={}", bucketName, key);
  }

  @Override
  public InputStream download(String key) {
    var request = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    var response = s3Client.getObject(request);
    log.debug("Downloaded object from S3: bucket={}, key={}", bucketName, key);
    return response;
  }

  @Override
  public boolean exists(String key) {
    try {
      s3Client.headObject(HeadObjectRequest.builder()
          .bucket(bucketName)
          .key(key)
          .build());
      return true;
    } catch (NoSuchKeyException e) {
      return false;
    }
  }

  @Override
  public void delete(String key) {
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    s3Client.deleteObject(request);
    log.debug("Deleted object from S3: bucket={}, key={}", bucketName, key);
  }

  @Override
  public String generatePresignedDownloadUrl(String key, Duration expiration) {
    var getObjectRequest = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    var presignRequest = GetObjectPresignRequest.builder()
        .signatureDuration(expiration)
        .getObjectRequest(getObjectRequest)
        .build();
    var presignedRequest = s3Presigner.presignGetObject(presignRequest);
    log.debug("Generated presigned download URL for: bucket={}, key={}", bucketName, key);
    return presignedRequest.url().toString();
  }

  @Override
  public String generatePresignedUploadUrl(String key, String contentType, Duration expiration) {
    var putObjectRequest = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(contentType)
        .build();
    var presignRequest = PutObjectPresignRequest.builder()
        .signatureDuration(expiration)
        .putObjectRequest(putObjectRequest)
        .build();
    var presignedRequest = s3Presigner.presignPutObject(presignRequest);
    log.debug("Generated presigned upload URL for: bucket={}, key={}", bucketName, key);
    return presignedRequest.url().toString();
  }

  // ==================== Extended Operations ====================

  /**
   * Get object metadata without downloading the content.
   *
   * @param key The S3 object key
   * @return Optional containing the metadata if object exists
   */
  public Optional<HeadObjectResponse> getMetadata(String key) {
    try {
      var response = s3Client.headObject(HeadObjectRequest.builder()
          .bucket(bucketName)
          .key(key)
          .build());
      return Optional.of(response);
    } catch (NoSuchKeyException e) {
      return Optional.empty();
    }
  }

  /**
   * Get the size of an object in bytes.
   *
   * @param key The S3 object key
   * @return Optional containing the size if object exists
   */
  public Optional<Long> getSize(String key) {
    return getMetadata(key).map(HeadObjectResponse::contentLength);
  }

  /**
   * Get the content type of an object.
   *
   * @param key The S3 object key
   * @return Optional containing the content type if object exists
   */
  public Optional<String> getContentType(String key) {
    return getMetadata(key).map(HeadObjectResponse::contentType);
  }

  /**
   * Upload with additional metadata.
   *
   * @param key           The S3 object key
   * @param inputStream   The file content
   * @param contentType   MIME type of the file
   * @param contentLength Size of the file in bytes
   * @param metadata      Custom metadata to attach to the object
   */
  public void uploadWithMetadata(String key, InputStream inputStream, String contentType,
      long contentLength, java.util.Map<String, String> metadata) {
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(contentType)
        .contentLength(contentLength)
        .metadata(metadata)
        .build();
    s3Client.putObject(request, RequestBody.fromInputStream(inputStream, contentLength));
    log.debug("Uploaded object with metadata to S3: bucket={}, key={}", bucketName, key);
  }

  /**
   * Copy an object within the same bucket.
   *
   * @param sourceKey      Source object key
   * @param destinationKey Destination object key
   */
  public void copy(String sourceKey, String destinationKey) {
    s3Client.copyObject(builder -> builder
        .sourceBucket(bucketName)
        .sourceKey(sourceKey)
        .destinationBucket(bucketName)
        .destinationKey(destinationKey));
    log.debug("Copied object in S3: {} -> {}", sourceKey, destinationKey);
  }

  // ==================== Helper Methods ====================

  /**
   * Build an S3 key from path segments.
   *
   * @param segments Path segments to join with "/"
   * @return The S3 object key
   */
  protected String buildKey(String... segments) {
    return String.join("/", segments);
  }

  /**
   * Get the bucket name this repository operates on.
   */
  public String getBucketName() {
    return bucketName;
  }
}
