package com.mediaservice.model;

public final class EventType {
  public static final String PROCESS_MEDIA = "media.v1.process";
  public static final String DELETE_MEDIA = "media.v1.delete";
  public static final String RESIZE_MEDIA = "media.v1.resize";

  private EventType() {
  }
}
