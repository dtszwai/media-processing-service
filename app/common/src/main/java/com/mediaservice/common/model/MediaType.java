package com.mediaservice.common.model;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonValue;

import java.util.Arrays;
import java.util.stream.Collectors;

public enum MediaType {
  IMAGE("image"),
  DOCUMENT("document"),
  VIDEO("video"),
  AUDIO("audio"),
  OTHER("other");

  private final String value;

  MediaType(String value) {
    this.value = value;
  }

  @JsonValue
  public String getValue() {
    return value;
  }

  @JsonCreator
  public static MediaType fromJson(String value) {
    if (value == null) {
      return null;
    }
    MediaType type = fromString(value);
    if (type == null) {
      String allowed = Arrays.stream(values())
          .map(MediaType::getValue)
          .collect(Collectors.joining(", "));
      throw new IllegalArgumentException("Invalid mediaType. Supported values: " + allowed + ".");
    }
    return type;
  }

  public static MediaType fromString(String value) {
    if (value == null || value.isBlank()) {
      return null;
    }
    for (MediaType type : values()) {
      if (type.value.equalsIgnoreCase(value) || type.name().equalsIgnoreCase(value)) {
        return type;
      }
    }
    return null;
  }
}
