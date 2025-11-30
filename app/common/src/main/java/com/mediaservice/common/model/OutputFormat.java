package com.mediaservice.common.model;

import com.fasterxml.jackson.annotation.JsonValue;

public enum OutputFormat {
  JPEG("jpeg", "image/jpeg"),
  PNG("png", "image/png"),
  WEBP("webp", "image/webp");

  private final String format;
  private final String contentType;

  OutputFormat(String format, String contentType) {
    this.format = format;
    this.contentType = contentType;
  }

  @JsonValue
  public String getFormat() {
    return format;
  }

  public String getContentType() {
    return contentType;
  }

  public String getExtension() {
    return "." + format;
  }

  public String applyToFileName(String originalName) {
    if (originalName == null || originalName.isEmpty()) {
      return "image" + getExtension();
    }
    int lastDot = originalName.lastIndexOf('.');
    String baseName = (lastDot > 0) ? originalName.substring(0, lastDot) : originalName;
    return baseName + getExtension();
  }

  public static OutputFormat fromString(String value) {
    if (value == null || value.isEmpty()) {
      return JPEG;
    }
    for (OutputFormat format : values()) {
      if (format.format.equalsIgnoreCase(value) || format.name().equalsIgnoreCase(value)) {
        return format;
      }
    }
    return JPEG;
  }
}
