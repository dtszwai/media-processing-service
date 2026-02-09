package com.mediaservice.media.infrastructure.storage;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.shared.config.properties.MediaProperties;
import com.mediaservice.shared.storage.AbstractS3StorageRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;

import java.io.IOException;
import java.time.Duration;

/**
 * S3 storage service for Media files.
 *
 * <p>Extends {@link AbstractS3StorageRepository} to inherit common S3 operations
 * while providing media-specific functionality like presigned URLs and format handling.
 *
 * <p>S3 Key Structure (tenant-scoped):
 * <pre>
 * {tenantId}/{mediaId}/assets/{assetId}.{ext}
 * </pre>
 *
 * @see StorageConstants
 */
@Slf4j
@Service
public class S3StorageService extends AbstractS3StorageRepository {
  private final MediaProperties mediaProperties;

  public S3StorageService(
      S3Client s3Client,
      S3Presigner s3Presigner,
      @Value("${aws.s3.bucket-name}") String bucketName,
      MediaProperties mediaProperties) {
    super(s3Client, s3Presigner, bucketName);
    this.mediaProperties = mediaProperties;
  }

  // ==================== Key Building ====================

  private String buildAssetKey(String tenantId, String mediaId, String assetId, String extension) {
    return StorageConstants.buildAssetKey(tenantId, mediaId, assetId, extension);
  }

  // ==================== Media-Specific Operations ====================

  /**
   * Upload a media file from a MultipartFile.
   */
  public void uploadAsset(String tenantId, String mediaId, String assetId, String mediaName, MultipartFile file) throws IOException {
    String extension = StorageConstants.getFileExtension(mediaName);
    String key = buildAssetKey(tenantId, mediaId, assetId, extension);
    upload(key, file.getInputStream(), file.getContentType(), file.getSize());
    log.info("Uploaded media to S3: {}", key);
  }

  /**
   * Get a presigned download URL for a media asset.
   */
  public String getAssetPresignedUrl(String tenantId, String mediaId, String assetId, String extension,
      String downloadName, String contentType) {
    String key = buildAssetKey(tenantId, mediaId, assetId, extension);
    Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
    String url = generatePresignedDownloadUrl(key, expiration, downloadName, contentType);
    log.info("Generated presigned URL for: {}", key);
    return url;
  }

  /**
   * Generate a presigned URL for uploading a media file directly to S3.
   */
  public String generatePresignedUploadUrl(String tenantId, String mediaId, String assetId, String fileName,
      String contentType, Duration expiration) {
    String extension = StorageConstants.getFileExtension(fileName);
    String key = buildAssetKey(tenantId, mediaId, assetId, extension);
    String url = generatePresignedUploadUrl(key, contentType, expiration);
    log.info("Generated presigned upload URL for: {}", key);
    return url;
  }

  /**
   * Check if a media asset exists.
   */
  public boolean assetExists(String tenantId, String mediaId, String assetId, String fileName) {
    String extension = StorageConstants.getFileExtension(fileName);
    String key = buildAssetKey(tenantId, mediaId, assetId, extension);
    return exists(key);
  }

  /**
   * Delete a media asset.
   */
  public void deleteAsset(String tenantId, String mediaId, String assetId, String fileName) {
    String extension = StorageConstants.getFileExtension(fileName);
    String key = buildAssetKey(tenantId, mediaId, assetId, extension);
    delete(key);
    log.info("Deleted upload from S3: {}", key);
  }
}
