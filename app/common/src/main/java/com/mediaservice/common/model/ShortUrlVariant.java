package com.mediaservice.common.model;

import java.util.Locale;

public enum ShortUrlVariant {
  THUMBNAIL("thumbnail"),
  DOWNLOAD("download"),
  ORIGINAL("original");

  private final String value;

  ShortUrlVariant(String value) {
    this.value = value;
  }

  public String getValue() {
    return value;
  }

  public static ShortUrlVariant fromString(String value) {
    if (value == null || value.isBlank()) {
      return null;
    }
    String normalized = value.trim().toLowerCase(Locale.ROOT);
    for (ShortUrlVariant variant : values()) {
      if (variant.value.equals(normalized)) {
        return variant;
      }
    }
    return null;
  }
}
