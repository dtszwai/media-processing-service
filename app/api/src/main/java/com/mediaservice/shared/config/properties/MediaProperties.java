package com.mediaservice.shared.config.properties;

import lombok.Data;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

@Data
@Component
@ConfigurationProperties(prefix = "media")
public class MediaProperties {
  private Width width = new Width();
  private long maxFileSize;
  private Upload upload = new Upload();

  @Data
  public static class Width {
    private int defaultValue = 500;
    private int min = 100;
    private int max = 1024;

    public int getDefault() {
      return defaultValue;
    }

    public void setDefault(int defaultValue) {
      this.defaultValue = defaultValue;
    }
  }

  @Data
  public static class Upload {
    private int presignedUrlExpirationSeconds = 3600; // 1 hour
    private long maxPresignedUploadSize = 1073741824L; // 1GB
    private int pendingUploadTtlHours = 24; // TTL for PENDING_UPLOAD records
  }

  @Data
  public static class Download {
    private int presignedUrlExpirationSeconds = 3600; // 1 hour
  }

  private Download download = new Download();

  public boolean isWidthValid(Integer width) {
    return width != null && width >= this.width.min && width <= this.width.max;
  }

  public int resolveWidth(Integer width) {
    return isWidthValid(width) ? width : this.width.defaultValue;
  }
}
