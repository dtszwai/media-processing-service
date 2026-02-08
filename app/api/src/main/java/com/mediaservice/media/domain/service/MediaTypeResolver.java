package com.mediaservice.media.domain.service;

import com.mediaservice.common.model.MediaType;
import org.springframework.stereotype.Service;

@Service
public class MediaTypeResolver {
  public MediaType resolve(MediaType requestedType, String contentType, String fileName) {
    if (requestedType != null) {
      return isSupported(requestedType) ? requestedType : null;
    }
    if (contentType != null) {
      if (contentType.startsWith("image/")) {
        return MediaType.IMAGE;
      }
      if (contentType.equalsIgnoreCase("application/pdf")) {
        return MediaType.DOCUMENT;
      }
    }
    if (fileName != null && fileName.toLowerCase().endsWith(".pdf")) {
      return MediaType.DOCUMENT;
    }
    return null;
  }

  private boolean isSupported(MediaType mediaType) {
    return mediaType == MediaType.IMAGE || mediaType == MediaType.DOCUMENT;
  }
}
