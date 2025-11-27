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

  private static final String KEY_PREFIX_UPLOADS = "uploads";
  private static final String KEY_PREFIX_RESIZED = "resized";
  private static final Duration PRESIGNED_URL_EXPIRATION = Duration.ofHours(1);

  private final S3Client s3Client;
  private final S3Presigner s3Presigner;

  @Value("${aws.s3.bucket-name}")
  private String bucketName;

  private String buildKey(String prefix, String mediaId, String fileName) {
    return String.join("/", prefix, mediaId, fileName);
  }

  public void uploadMedia(String mediaId, String mediaName, MultipartFile file) throws IOException {
    String key = buildKey(KEY_PREFIX_UPLOADS, mediaId, mediaName);
    PutObjectRequest request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(file.getContentType())
        .contentLength(file.getSize())
        .build();
    s3Client.putObject(request, RequestBody.fromInputStream(file.getInputStream(), file.getSize()));
    log.info("Uploaded media to S3: {}", key);
  }

  public String getPresignedUrl(String mediaId, String mediaName) {
    String key = buildKey(KEY_PREFIX_RESIZED, mediaId, mediaName);
    GetObjectRequest getObjectRequest = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    GetObjectPresignRequest presignRequest = GetObjectPresignRequest.builder()
        .signatureDuration(PRESIGNED_URL_EXPIRATION)
        .getObjectRequest(getObjectRequest)
        .build();
    PresignedGetObjectRequest presignedRequest = s3Presigner.presignGetObject(presignRequest);
    log.info("Generated presigned URL for: {}", key);
    return presignedRequest.url().toString();
  }
}
