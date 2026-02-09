package com.mediaservice.common.model;

import com.fasterxml.jackson.annotation.JsonValue;

public enum AssetOperation {
  IMAGE_PROCESS("image.process"),
  IMAGE_PREVIEW("image.preview"),
  DOCUMENT_PREVIEW("document.preview"),
  DOCUMENT_TEXT("document.text");

  private final String value;

  AssetOperation(String value) {
    this.value = value;
  }

  @JsonValue
  public String getValue() {
    return value;
  }

  public static AssetOperation fromString(String value) {
    if (value == null) {
      return null;
    }
    for (AssetOperation op : values()) {
      if (op.value.equalsIgnoreCase(value) || op.name().equalsIgnoreCase(value)) {
        return op;
      }
    }
    return null;
  }
}
