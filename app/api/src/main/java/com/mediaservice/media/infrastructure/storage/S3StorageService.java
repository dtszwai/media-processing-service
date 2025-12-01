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

/**
 * S3 storage service for Media files.
 *
 * <p>Extends {@link AbstractS3StorageRepository} to inherit common S3 operations
 * while providing media-specific functionality like presigned URLs and format handling.
 *
 * <p>S3 Key Structure (flat, mediaId-first):
 * <pre>
 * {mediaId}/
 *   original.{ext}   - Original uploaded file
 *   processed.{ext}  - Processed/resized output
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

  // ==================== Media-Specific Operations ====================

  /**
   * Build the S3 key for an original media file.
   * Format: {mediaId}/original.{ext}
   */
  private String buildOriginalKey(String mediaId, String fileName) {
    String extension = StorageConstants.getFileExtension(fileName);
    return StorageConstants.buildS3Key(mediaId, StorageConstants.S3_VARIANT_ORIGINAL, extension);
  }

  /**
   * Build the S3 key for a processed media file.
   * Format: {mediaId}/processed.{ext}
   */
  private String buildProcessedKey(String mediaId, OutputFormat format) {
    return StorageConstants.buildS3Key(mediaId, StorageConstants.S3_VARIANT_PROCESSED, format.getExtension());
  }

  /**
   * Upload a media file from a MultipartFile.
   *
   * @param mediaId   The media ID
   * @param mediaName The original file name (used to determine extension)
   * @param file      The uploaded file
   */
  public void uploadMedia(String mediaId, String mediaName, MultipartFile file) throws IOException {
    String key = buildOriginalKey(mediaId, mediaName);
    upload(key, file.getInputStream(), file.getContentType(), file.getSize());
    log.info("Uploaded media to S3: {}", key);
  }

  /**
   * Get a presigned download URL for a processed media file.
   *
   * @param mediaId      The media ID
   * @param mediaName    The original file name (unused, kept for API compatibility)
   * @param outputFormat The output format (determines file extension)
   * @return The presigned download URL
   */
  public String getPresignedUrl(String mediaId, String mediaName, OutputFormat outputFormat) {
    OutputFormat format = outputFormat != null ? outputFormat : OutputFormat.JPEG;
    String key = buildProcessedKey(mediaId, format);
    Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
    String url = generatePresignedDownloadUrl(key, expiration);
    log.info("Generated presigned URL for: {}", key);
    return url;
  }

  /**
   * Generate a presigned URL for uploading a media file directly to S3.
   *
   * @param mediaId     The media ID
   * @param fileName    The file name (used to determine extension)
   * @param contentType The expected MIME type
   * @param expiration  How long the URL should be valid
   * @return The presigned upload URL
   */
  public String generatePresignedUploadUrl(String mediaId, String fileName, String contentType, Duration expiration) {
    String key = buildOriginalKey(mediaId, fileName);
    String url = generatePresignedUploadUrl(key, contentType, expiration);
    log.info("Generated presigned upload URL for: {}", key);
    return url;
  }

  /**
   * Check if an uploaded (original) media file exists.
   *
   * @param mediaId  The media ID
   * @param fileName The file name (used to determine extension)
   * @return true if the file exists
   */
  public boolean objectExists(String mediaId, String fileName) {
    String key = buildOriginalKey(mediaId, fileName);
    return exists(key);
  }

  /**
   * Delete an uploaded (original) media file.
   *
   * @param mediaId  The media ID
   * @param fileName The file name (used to determine extension)
   */
  public void deleteUpload(String mediaId, String fileName) {
    String key = buildOriginalKey(mediaId, fileName);
    delete(key);
    log.info("Deleted upload from S3: {}", key);
  }
}
