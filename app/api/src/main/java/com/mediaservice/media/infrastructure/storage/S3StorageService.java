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
 * <p>
 * Extends {@link AbstractS3StorageRepository} to inherit common S3 operations
 * while providing media-specific functionality like presigned URLs and format
 * handling.
 *
 * <p>
 * Key prefixes:
 * <ul>
 * <li>{@code uploads/} - Original uploaded files</li>
 * <li>{@code resized/} - Processed/resized files</li>
 * </ul>
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
   * Build the S3 key for a media file.
   */
  private String buildMediaKey(String prefix, String mediaId, String fileName) {
    return buildKey(prefix, mediaId, fileName);
  }

  /**
   * Upload a media file from a MultipartFile.
   *
   * @param mediaId   The media ID
   * @param mediaName The file name
   * @param file      The uploaded file
   */
  public void uploadMedia(String mediaId, String mediaName, MultipartFile file) throws IOException {
    String key = buildMediaKey(StorageConstants.S3_PREFIX_UPLOADS, mediaId, mediaName);
    upload(key, file.getInputStream(), file.getContentType(), file.getSize());
    log.info("Uploaded media to S3: {}", key);
  }

  /**
   * Get a presigned download URL for a processed media file.
   *
   * @param mediaId      The media ID
   * @param mediaName    The original file name
   * @param outputFormat The output format (determines file extension)
   * @return The presigned download URL
   */
  public String getPresignedUrl(String mediaId, String mediaName, OutputFormat outputFormat) {
    OutputFormat format = outputFormat != null ? outputFormat : OutputFormat.JPEG;
    String outputFileName = format.applyToFileName(mediaName);
    String key = buildMediaKey(StorageConstants.S3_PREFIX_RESIZED, mediaId, outputFileName);
    Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
    String url = generatePresignedDownloadUrl(key, expiration);
    log.info("Generated presigned URL for: {}", key);
    return url;
  }

  /**
   * Generate a presigned URL for uploading a media file directly to S3.
   *
   * @param mediaId     The media ID
   * @param fileName    The file name
   * @param contentType The expected MIME type
   * @param expiration  How long the URL should be valid
   * @return The presigned upload URL
   */
  public String generatePresignedUploadUrl(String mediaId, String fileName, String contentType, Duration expiration) {
    String key = buildMediaKey(StorageConstants.S3_PREFIX_UPLOADS, mediaId, fileName);
    String url = generatePresignedUploadUrl(key, contentType, expiration);
    log.info("Generated presigned upload URL for: {}", key);
    return url;
  }

  /**
   * Check if an uploaded media file exists.
   *
   * @param mediaId  The media ID
   * @param fileName The file name
   * @return true if the file exists
   */
  public boolean objectExists(String mediaId, String fileName) {
    String key = buildMediaKey(StorageConstants.S3_PREFIX_UPLOADS, mediaId, fileName);
    return exists(key);
  }

  /**
   * Delete an uploaded media file.
   *
   * @param mediaId  The media ID
   * @param fileName The file name
   */
  public void deleteUpload(String mediaId, String fileName) {
    String key = buildMediaKey(StorageConstants.S3_PREFIX_UPLOADS, mediaId, fileName);
    delete(key);
    log.info("Deleted upload from S3: {}", key);
  }
}
