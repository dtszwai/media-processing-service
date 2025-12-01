package com.mediaservice.shared.storage;

import java.io.InputStream;
import java.time.Duration;

/**
 * Repository interface for S3 storage operations.
 *
 * <p>
 * Defines the contract for S3 storage repositories following the Repository
 * pattern.
 * This separates file/blob storage concerns from structured data persistence.
 *
 * <p>
 * Key distinction from
 * {@link com.mediaservice.shared.persistence.DynamoDbRepository}:
 * <ul>
 * <li><b>Storage</b> (S3) - Unstructured data: files, images, binaries</li>
 * <li><b>Persistence</b> (DynamoDB) - Structured data: records, metadata</li>
 * </ul>
 */
public interface S3StorageRepository {
  /**
   * Upload a file to S3.
   *
   * @param key           The S3 object key
   * @param inputStream   The file content
   * @param contentType   MIME type of the file
   * @param contentLength Size of the file in bytes
   */
  void upload(String key, InputStream inputStream, String contentType, long contentLength);

  /**
   * Download a file from S3.
   *
   * @param key The S3 object key
   * @return InputStream to read the file content
   */
  InputStream download(String key);

  /**
   * Check if an object exists in S3.
   *
   * @param key The S3 object key
   * @return true if the object exists
   */
  boolean exists(String key);

  /**
   * Delete an object from S3.
   *
   * @param key The S3 object key
   */
  void delete(String key);

  /**
   * Generate a presigned URL for downloading an object.
   *
   * @param key        The S3 object key
   * @param expiration How long the URL should be valid
   * @return The presigned URL
   */
  String generatePresignedDownloadUrl(String key, Duration expiration);

  /**
   * Generate a presigned URL for uploading an object.
   *
   * @param key         The S3 object key
   * @param contentType Expected MIME type of the upload
   * @param expiration  How long the URL should be valid
   * @return The presigned URL
   */
  String generatePresignedUploadUrl(String key, String contentType, Duration expiration);
}
