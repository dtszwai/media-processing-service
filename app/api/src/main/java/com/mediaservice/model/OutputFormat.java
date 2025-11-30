package com.mediaservice.model;

import com.fasterxml.jackson.annotation.JsonValue;

/**
 * Supported output formats for processed images.
 */
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

  public static OutputFormat fromString(String value) {
    if (value == null || value.isEmpty()) {
      return JPEG; // default
    }
    for (OutputFormat format : values()) {
      if (format.format.equalsIgnoreCase(value) || format.name().equalsIgnoreCase(value)) {
        return format;
      }
    }
    return JPEG; // default fallback
  }
}
