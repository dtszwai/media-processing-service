package com.mediaservice.common.model;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonValue;

public enum MediaSource {
  UPLOAD("upload"),
  GENERATED("generated");

  private final String value;

  MediaSource(String value) {
    this.value = value;
  }

  @JsonValue
  public String getValue() {
    return value;
  }

  @JsonCreator
  public static MediaSource fromString(String value) {
    if (value == null || value.isBlank()) {
      return UPLOAD;
    }
    for (MediaSource source : values()) {
      if (source.value.equalsIgnoreCase(value) || source.name().equalsIgnoreCase(value)) {
        return source;
      }
    }
    throw new IllegalArgumentException("Invalid media source: " + value);
  }
}
