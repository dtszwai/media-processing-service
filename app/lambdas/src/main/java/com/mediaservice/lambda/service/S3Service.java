package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.OutputFormat;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.DeleteObjectRequest;
import software.amazon.awssdk.services.s3.model.GetObjectRequest;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

/**
 * S3 service for Lambda media operations.
 *
 * <p>S3 Key Structure (flat, mediaId-first):
 * <pre>
 * {mediaId}/
 *   original.{ext}   - Original uploaded file
 *   processed.{ext}  - Processed/resized output
 * </pre>
 */
public class S3Service {
  private final S3Client client;
  private final String bucketName;

  public S3Service() {
    this.client = AwsClientFactory.getS3Client();
    this.bucketName = LambdaConfig.getInstance().getBucketName();
  }

  /**
   * Get the original uploaded media file.
   *
   * @param mediaId   The media ID
   * @param mediaName The original filename (used to determine extension)
   * @return The file contents as byte array
   */
  public byte[] getMediaFile(String mediaId, String mediaName) {
    String extension = StorageConstants.getFileExtension(mediaName);
    String key = StorageConstants.buildS3Key(mediaId, StorageConstants.S3_VARIANT_ORIGINAL, extension);
    var request = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    return client.getObjectAsBytes(request).asByteArray();
  }

  /**
   * Upload a processed media file.
   *
   * @param mediaId      The media ID
   * @param mediaName    The original filename (unused in new structure)
   * @param data         The processed image data
   * @param outputFormat The output format (determines extension)
   */
  public void uploadProcessedMedia(String mediaId, String mediaName, byte[] data, OutputFormat outputFormat) {
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;
    String key = StorageConstants.buildS3Key(mediaId, StorageConstants.S3_VARIANT_PROCESSED, format.getExtension());
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(format.getContentType())
        .build();
    client.putObject(request, RequestBody.fromBytes(data));
  }

  /**
   * Delete the original uploaded media file.
   *
   * @param mediaId   The media ID
   * @param mediaName The original filename (used to determine extension)
   */
  public void deleteOriginalFile(String mediaId, String mediaName) {
    String extension = StorageConstants.getFileExtension(mediaName);
    String key = StorageConstants.buildS3Key(mediaId, StorageConstants.S3_VARIANT_ORIGINAL, extension);
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }

  /**
   * Delete the processed media file.
   *
   * @param mediaId      The media ID
   * @param outputFormat The output format (determines extension)
   */
  public void deleteProcessedFile(String mediaId, OutputFormat outputFormat) {
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;
    String key = StorageConstants.buildS3Key(mediaId, StorageConstants.S3_VARIANT_PROCESSED, format.getExtension());
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }
}
