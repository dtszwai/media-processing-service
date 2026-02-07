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
 * <p>S3 Key Structure (tenant-scoped):
 * <pre>
 * {tenantId}/{mediaId}/
 *   original.{ext}   - Original uploaded file
 *   processed.{ext}  - Processed/resized output
 *   preview.{ext}    - Watermarked preview for CDN
 * </pre>
 */
public class S3Service {
  private final S3Client client;
  private final String bucketName;

  public S3Service() {
    this.client = AwsClientFactory.getS3Client();
    this.bucketName = LambdaConfig.getInstance().getBucketName();
  }

  /** Constructor for testing. */
  S3Service(S3Client client, String bucketName) {
    this.client = client;
    this.bucketName = bucketName;
  }

  /**
   * Get the original uploaded media file.
   */
  public byte[] getMediaFile(String tenantId, String mediaId, String mediaName) {
    String extension = StorageConstants.getFileExtension(mediaName);
    String key = StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.S3_VARIANT_ORIGINAL, extension);
    var request = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    return client.getObjectAsBytes(request).asByteArray();
  }

  /**
   * Upload a processed media file.
   */
  public void uploadProcessedMedia(String tenantId, String mediaId, String mediaName, byte[] data, OutputFormat outputFormat) {
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;
    String key = StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.S3_VARIANT_PROCESSED, format.getExtension());
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(format.getContentType())
        .build();
    client.putObject(request, RequestBody.fromBytes(data));
  }

  /**
   * Delete the original uploaded media file.
   */
  public void deleteOriginalFile(String tenantId, String mediaId, String mediaName) {
    String extension = StorageConstants.getFileExtension(mediaName);
    String key = StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.S3_VARIANT_ORIGINAL, extension);
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }

  /**
   * Delete the processed media file.
   */
  public void deleteProcessedFile(String tenantId, String mediaId, OutputFormat outputFormat) {
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;
    String key = StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.S3_VARIANT_PROCESSED, format.getExtension());
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }

  /**
   * Upload a preview image for CDN distribution.
   * Preview has 1-year cache control for efficient CDN caching.
   */
  public void uploadPreview(String tenantId, String mediaId, byte[] previewData, OutputFormat outputFormat) {
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;
    String key = StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.VARIANT_PREVIEW, format.getExtension());
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(format.getContentType())
        .cacheControl("public, max-age=31536000")  // 1 year cache for CDN
        .build();
    client.putObject(request, RequestBody.fromBytes(previewData));
  }

  /**
   * Delete the preview media file.
   */
  public void deletePreviewFile(String tenantId, String mediaId, OutputFormat outputFormat) {
    OutputFormat format = (outputFormat != null) ? outputFormat : OutputFormat.JPEG;
    String key = StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.VARIANT_PREVIEW, format.getExtension());
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }
}
