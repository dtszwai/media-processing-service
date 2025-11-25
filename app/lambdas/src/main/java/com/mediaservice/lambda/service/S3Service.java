package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.*;

public class S3Service {

  private static final Logger logger = LoggerFactory.getLogger(S3Service.class);
  private static final String BUCKET_NAME = System.getenv("MEDIA_BUCKET_NAME") != null
      ? System.getenv("MEDIA_BUCKET_NAME")
      : "media-bucket";

  private final S3Client client;

  public S3Service() {
    this.client = AwsClientFactory.getS3Client();
  }

  public byte[] getMediaFile(String mediaId, String mediaName) {
    String key = String.format("uploads/%s/%s", mediaId, mediaName);

    GetObjectRequest request = GetObjectRequest.builder()
        .bucket(BUCKET_NAME)
        .key(key)
        .build();

    try {
      byte[] data = client.getObjectAsBytes(request).asByteArray();
      logger.info("Retrieved media file from S3: {}", key);
      return data;
    } catch (S3Exception e) {
      logger.error("Failed to get media file from S3: {}", e.getMessage());
      throw e;
    }
  }

  public void uploadMedia(String mediaId, String mediaName, byte[] data, String keyPrefix) {
    // Processed images are always JPEG, so normalize extension to .jpg
    String processedFileName = mediaName.replaceAll("\\.[^.]+$", ".jpg");
    String key = String.format("%s/%s/%s", keyPrefix, mediaId, processedFileName);

    PutObjectRequest request = PutObjectRequest.builder()
        .bucket(BUCKET_NAME)
        .key(key)
        .contentType("image/jpeg")
        .build();

    try {
      client.putObject(request, RequestBody.fromBytes(data));
      logger.info("Uploaded processed media to S3: {}", key);
    } catch (S3Exception e) {
      logger.error("Failed to upload processed media to S3: {}", e.getMessage());
      throw e;
    }
  }

  public void deleteMediaFile(String mediaId, String mediaName, String keyPrefix) {
    // For resized files, use .jpg extension (processed images are always JPEG)
    String fileName = "resized".equals(keyPrefix)
        ? mediaName.replaceAll("\\.[^.]+$", ".jpg")
        : mediaName;
    String key = String.format("%s/%s/%s", keyPrefix, mediaId, fileName);

    DeleteObjectRequest request = DeleteObjectRequest.builder()
        .bucket(BUCKET_NAME)
        .key(key)
        .build();

    try {
      client.deleteObject(request);
      logger.info("Deleted media from S3: {}", key);
    } catch (S3Exception e) {
      logger.error("Failed to delete media from S3: {}", e.getMessage());
      throw e;
    }
  }
}
