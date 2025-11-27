package com.mediaservice.lambda.service;

import com.mediaservice.lambda.config.AwsClientFactory;
import com.mediaservice.lambda.config.LambdaConfig;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.DeleteObjectRequest;
import software.amazon.awssdk.services.s3.model.GetObjectRequest;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

public class S3Service {
  private final S3Client client;
  private final String bucketName;

  public S3Service() {
    this.client = AwsClientFactory.getS3Client();
    this.bucketName = LambdaConfig.getInstance().getBucketName();
  }

  public byte[] getMediaFile(String mediaId, String mediaName) {
    var key = String.join("/", "uploads", mediaId, mediaName);
    var request = GetObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    return client.getObjectAsBytes(request).asByteArray();
  }

  public void uploadMedia(String mediaId, String mediaName, byte[] data, String keyPrefix) {
    var key = String.join("/", keyPrefix, mediaId, mediaName);
    var request = PutObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .contentType("image/jpeg")
        .build();
    client.putObject(request, RequestBody.fromBytes(data));
  }

  public void deleteMediaFile(String mediaId, String mediaName, String keyPrefix) {
    var key = String.join("/", keyPrefix, mediaId, mediaName);
    var request = DeleteObjectRequest.builder()
        .bucket(bucketName)
        .key(key)
        .build();
    client.deleteObject(request);
  }
}
