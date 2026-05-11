package com.mediaservice.generation.infrastructure;

import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.media.infrastructure.storage.S3StorageService;
import com.mediaservice.providers.generation.core.GeneratedAssetStorage;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Component;

@Component
@RequiredArgsConstructor
public class ApiGeneratedAssetStorage implements GeneratedAssetStorage {
  private final S3StorageService s3StorageService;

  @Override
  public void put(String tenantId, String mediaId, String assetId, Artifact artifact) {
    s3StorageService.uploadAssetBytes(tenantId, mediaId, assetId, artifact.extension(), artifact.bytes(),
        artifact.contentType(), true);
  }

  @Override
  public String presignedUrl(String tenantId, String mediaId, String assetId, String extension, String downloadName,
      String contentType) {
    return s3StorageService.getAssetPresignedUrl(tenantId, mediaId, assetId, extension, downloadName, contentType);
  }
}
