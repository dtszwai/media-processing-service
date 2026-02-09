package com.mediaservice.lambda.service;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
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
 * {tenantId}/{mediaId}/assets/{assetId}.{ext}
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

  public byte[] downloadAsset(String tenantId, String mediaId, String assetId, String extension) {
    String key = StorageConstants.buildAssetKey(tenantId, mediaId, assetId, extension);
    var request = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    return client.getObjectAsBytes(request).asByteArray();
  }

  public void uploadAsset(String tenantId, String mediaId, String assetId, String extension, byte[] data,
      String contentType, boolean cachePublic) {
    String key = StorageConstants.buildAssetKey(tenantId, mediaId, assetId, extension);
    var requestBuilder = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(contentType);
    if (cachePublic) {
      requestBuilder.cacheControl("public, max-age=31536000");
    }
    client.putObject(requestBuilder.build(), RequestBody.fromBytes(data));
  }

  public void deleteAsset(String tenantId, String mediaId, String assetId, String extension) {
    String key = StorageConstants.buildAssetKey(tenantId, mediaId, assetId, extension);
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }
}
