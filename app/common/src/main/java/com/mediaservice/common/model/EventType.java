package com.mediaservice.common.model;

import com.fasterxml.jackson.annotation.JsonValue;

public enum EventType {
  PROCESS_MEDIA("media.v1.process"),
  DELETE_MEDIA("media.v1.delete"),
  RESIZE_MEDIA("media.v1.resize");

  private final String value;

  EventType(String value) {
    this.value = value;
  }

  @JsonValue
  public String getValue() {
    return value;
  }

  public static EventType fromString(String value) {
    if (value == null) {
      return null;
    }
    for (EventType type : values()) {
      if (type.value.equals(value)) {
        return type;
      }
    }
    return null;
  }
}
