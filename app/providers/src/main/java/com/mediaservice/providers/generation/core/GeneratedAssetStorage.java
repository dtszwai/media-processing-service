package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.provider.Artifact;

public interface GeneratedAssetStorage {
  void put(String tenantId, String mediaId, String assetId, Artifact artifact);

  String presignedUrl(String tenantId, String mediaId, String assetId, String extension, String downloadName,
      String contentType);
}
