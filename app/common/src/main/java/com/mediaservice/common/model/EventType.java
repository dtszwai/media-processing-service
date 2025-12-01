package com.mediaservice.common.model;

import com.fasterxml.jackson.annotation.JsonValue;

public enum EventType {
  // Media processing events
  PROCESS_MEDIA("media.v1.process"),
  DELETE_MEDIA("media.v1.delete"),
  RESIZE_MEDIA("media.v1.resize"),

  // Analytics rollup events (triggered by EventBridge schedules)
  DAILY_ROLLUP("analytics.v1.rollup.daily"),
  WEEKLY_ROLLUP("analytics.v1.rollup.weekly"),
  MONTHLY_ROLLUP("analytics.v1.rollup.monthly"),
  YEARLY_ROLLUP("analytics.v1.rollup.yearly"),

  // Analytics archive events (triggered by EventBridge schedules)
  DAILY_ARCHIVE("analytics.v1.archive.daily"),
  MONTHLY_ARCHIVE("analytics.v1.archive.monthly");

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
