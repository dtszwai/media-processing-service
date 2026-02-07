package com.mediaservice.media.infrastructure.storage;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.model.OutputFormat;
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
import java.util.Optional;

/**
 * S3 storage service for Media files.
 *
 * <p>Extends {@link AbstractS3StorageRepository} to inherit common S3 operations
 * while providing media-specific functionality like presigned URLs and format handling.
 *
 * <p>S3 Key Structure (tenant-scoped):
 * <pre>
 * {tenantId}/{mediaId}/
 *   original.{ext}   - Original uploaded file
 *   processed.{ext}  - Processed/resized output
 *   preview.{ext}    - Watermarked preview for CDN
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

  private String buildOriginalKey(String tenantId, String mediaId, String fileName) {
    String extension = StorageConstants.getFileExtension(fileName);
    return StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.S3_VARIANT_ORIGINAL, extension);
  }

  private String buildProcessedKey(String tenantId, String mediaId, OutputFormat format) {
    return StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.S3_VARIANT_PROCESSED, format.getExtension());
  }

  private String buildPreviewKey(String tenantId, String mediaId, OutputFormat format) {
    return StorageConstants.buildS3Key(tenantId, mediaId, StorageConstants.VARIANT_PREVIEW, format.getExtension());
  }

  // ==================== Media-Specific Operations ====================

  /**
   * Upload a media file from a MultipartFile.
   */
  public void uploadMedia(String tenantId, String mediaId, String mediaName, MultipartFile file) throws IOException {
    String key = buildOriginalKey(tenantId, mediaId, mediaName);
    upload(key, file.getInputStream(), file.getContentType(), file.getSize());
    log.info("Uploaded media to S3: {}", key);
  }

  /**
   * Get a presigned download URL for a processed media file.
   */
  public String getPresignedUrl(String tenantId, String mediaId, String mediaName, OutputFormat outputFormat) {
    OutputFormat format = outputFormat != null ? outputFormat : OutputFormat.JPEG;
    String key = buildProcessedKey(tenantId, mediaId, format);
    Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
    String url = generatePresignedDownloadUrl(key, expiration);
    log.info("Generated presigned URL for: {}", key);
    return url;
  }

  /**
   * Generate a presigned URL for uploading a media file directly to S3.
   */
  public String generatePresignedUploadUrl(String tenantId, String mediaId, String fileName, String contentType, Duration expiration) {
    String key = buildOriginalKey(tenantId, mediaId, fileName);
    String url = generatePresignedUploadUrl(key, contentType, expiration);
    log.info("Generated presigned upload URL for: {}", key);
    return url;
  }

  /**
   * Check if an uploaded (original) media file exists.
   */
  public boolean objectExists(String tenantId, String mediaId, String fileName) {
    String key = buildOriginalKey(tenantId, mediaId, fileName);
    return exists(key);
  }

  /**
   * Delete an uploaded (original) media file.
   */
  public void deleteUpload(String tenantId, String mediaId, String fileName) {
    String key = buildOriginalKey(tenantId, mediaId, fileName);
    delete(key);
    log.info("Deleted upload from S3: {}", key);
  }

  /**
   * Get a presigned download URL for the original media file.
   */
  public String getOriginalPresignedUrl(String tenantId, String mediaId, String fileName) {
    String key = buildOriginalKey(tenantId, mediaId, fileName);
    Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
    String url = generatePresignedDownloadUrl(key, expiration);
    log.info("Generated presigned URL for original: {}", key);
    return url;
  }

  /**
   * Get a presigned download URL for a preview media file.
   * Used as fallback when CloudFront is not configured.
   */
  public Optional<String> getPreviewPresignedUrl(String tenantId, String mediaId, OutputFormat outputFormat) {
    try {
      OutputFormat format = outputFormat != null ? outputFormat : OutputFormat.JPEG;
      String key = buildPreviewKey(tenantId, mediaId, format);
      Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
      String url = generatePresignedDownloadUrl(key, expiration);
      log.info("Generated presigned preview URL for: {}", key);
      return Optional.of(url);
    } catch (Exception e) {
      log.warn("Failed to generate preview presigned URL for mediaId={}: {}", mediaId, e.getMessage());
      return Optional.empty();
    }
  }
}
