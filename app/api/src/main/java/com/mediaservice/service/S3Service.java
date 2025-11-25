package com.mediaservice.service;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.*;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;
import software.amazon.awssdk.services.s3.presigner.model.GetObjectPresignRequest;
import software.amazon.awssdk.services.s3.presigner.model.PresignedGetObjectRequest;

import java.io.IOException;
import java.time.Duration;

@Slf4j
@Service
@RequiredArgsConstructor
public class S3Service {

  private final S3Client s3Client;
  private final S3Presigner s3Presigner;

  @Value("${aws.s3.bucket-name}")
  private String bucketName;

  private static final Duration PRESIGNED_URL_EXPIRATION = Duration.ofHours(1);

  public void uploadMedia(String mediaId, String mediaName, MultipartFile file) throws IOException {
    String key = String.format("uploads/%s/%s", mediaId, mediaName);

    PutObjectRequest request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(file.getContentType())
        .contentLength(file.getSize())
        .build();

    try {
      s3Client.putObject(request, RequestBody.fromInputStream(file.getInputStream(), file.getSize()));
      log.info("Uploaded media to S3: {}", key);
    } catch (S3Exception e) {
      log.error("Failed to upload media to S3: {}", e.getMessage());
      throw e;
    }
  }

  public void uploadMedia(String mediaId, String mediaName, byte[] data, String keyPrefix) {
    String key = String.format("%s/%s/%s", keyPrefix, mediaId, mediaName);

    PutObjectRequest request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType("image/jpeg")
        .build();

    try {
      s3Client.putObject(request, RequestBody.fromBytes(data));
      log.info("Uploaded processed media to S3: {}", key);
    } catch (S3Exception e) {
      log.error("Failed to upload processed media to S3: {}", e.getMessage());
      throw e;
    }
  }

  public String getPresignedUrl(String mediaId, String mediaName) {
    // Processed images are always saved as .jpg
    String processedFileName = mediaName.replaceAll("\\.[^.]+$", ".jpg");
    String key = String.format("resized/%s/%s", mediaId, processedFileName);

    GetObjectRequest getObjectRequest = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();

    GetObjectPresignRequest presignRequest = GetObjectPresignRequest.builder()
        .signatureDuration(PRESIGNED_URL_EXPIRATION)
        .getObjectRequest(getObjectRequest)
        .build();

    try {
      PresignedGetObjectRequest presignedRequest = s3Presigner.presignGetObject(presignRequest);
      String url = presignedRequest.url().toString();
      log.info("Generated presigned URL for: {}", key);
      return url;
    } catch (S3Exception e) {
      log.error("Failed to generate presigned URL: {}", e.getMessage());
      throw e;
    }
  }

  public byte[] getMediaFile(String mediaId, String mediaName) {
    String key = String.format("uploads/%s/%s", mediaId, mediaName);

    GetObjectRequest request = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();

    try {
      return s3Client.getObjectAsBytes(request).asByteArray();
    } catch (S3Exception e) {
      log.error("Failed to get media file from S3: {}", e.getMessage());
      throw e;
    }
  }

  public void deleteMediaFile(String mediaId, String mediaName, String keyPrefix) {
    String key = String.format("%s/%s/%s", keyPrefix, mediaId, mediaName);

    DeleteObjectRequest request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();

    try {
      s3Client.deleteObject(request);
      log.info("Deleted media from S3: {}", key);
    } catch (S3Exception e) {
      log.error("Failed to delete media from S3: {}", e.getMessage());
      throw e;
    }
  }
}
