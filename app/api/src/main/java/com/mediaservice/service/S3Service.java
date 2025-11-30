package com.mediaservice.service;

import com.mediaservice.config.MediaProperties;
import com.mediaservice.common.model.OutputFormat;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.GetObjectRequest;
import software.amazon.awssdk.services.s3.model.HeadObjectRequest;
import software.amazon.awssdk.services.s3.model.NoSuchKeyException;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;
import software.amazon.awssdk.services.s3.presigner.S3Presigner;
import software.amazon.awssdk.services.s3.presigner.model.GetObjectPresignRequest;
import software.amazon.awssdk.services.s3.presigner.model.PutObjectPresignRequest;

import java.io.IOException;
import java.time.Duration;

@Slf4j
@Service
public class S3Service {
  private static final String KEY_PREFIX_UPLOADS = "uploads";
  private static final String KEY_PREFIX_RESIZED = "resized";

  private final S3Client s3Client;
  private final S3Presigner s3Presigner;
  private final MediaProperties mediaProperties;

  @Value("${aws.s3.bucket-name}")
  private String bucketName;

  public S3Service(S3Client s3Client, S3Presigner s3Presigner, MediaProperties mediaProperties) {
    this.s3Client = s3Client;
    this.s3Presigner = s3Presigner;
    this.mediaProperties = mediaProperties;
  }

  private String buildKey(String prefix, String mediaId, String fileName) {
    return String.join("/", prefix, mediaId, fileName);
  }

  public void uploadMedia(String mediaId, String mediaName, MultipartFile file) throws IOException {
    String key = buildKey(KEY_PREFIX_UPLOADS, mediaId, mediaName);
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(file.getContentType())
        .contentLength(file.getSize())
        .build();
    s3Client.putObject(request, RequestBody.fromInputStream(file.getInputStream(), file.getSize()));
    log.info("Uploaded media to S3: {}", key);
  }

  public String getPresignedUrl(String mediaId, String mediaName, OutputFormat outputFormat) {
    OutputFormat format = outputFormat != null ? outputFormat : OutputFormat.JPEG;
    String outputFileName = format.applyToFileName(mediaName);
    String key = buildKey(KEY_PREFIX_RESIZED, mediaId, outputFileName);
    var getObjectRequest = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    Duration expiration = Duration.ofSeconds(mediaProperties.getDownload().getPresignedUrlExpirationSeconds());
    var presignRequest = GetObjectPresignRequest.builder()
        .signatureDuration(expiration)
        .getObjectRequest(getObjectRequest)
        .build();
    var presignedRequest = s3Presigner.presignGetObject(presignRequest);
    log.info("Generated presigned URL for: {}", key);
    return presignedRequest.url().toString();
  }

  public String generatePresignedUploadUrl(String mediaId, String fileName, String contentType, Duration expiration) {
    String key = buildKey(KEY_PREFIX_UPLOADS, mediaId, fileName);
    var putObjectRequest = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType(contentType)
        .build();
    var presignRequest = PutObjectPresignRequest.builder()
        .signatureDuration(expiration)
        .putObjectRequest(putObjectRequest)
        .build();
    var presignedRequest = s3Presigner.presignPutObject(presignRequest);
    log.info("Generated presigned upload URL for: {}", key);
    return presignedRequest.url().toString();
  }

  public boolean objectExists(String mediaId, String fileName) {
    String key = buildKey(KEY_PREFIX_UPLOADS, mediaId, fileName);
    try {
      s3Client.headObject(HeadObjectRequest.builder()
          .bucket(bucketName)
          .key(key)
          .build());
      return true;
    } catch (NoSuchKeyException e) {
      return false;
    }
  }
}
