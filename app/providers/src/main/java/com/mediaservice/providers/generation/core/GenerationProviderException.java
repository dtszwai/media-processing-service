package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationErrorCode;

public class GenerationProviderException extends RuntimeException {
  private final String code;

  public GenerationProviderException(GenerationErrorCode code, String message) {
    super(message);
    this.code = code.name();
  }

  public GenerationProviderException(String code, String message) {
    super(message);
    this.code = code;
  }

  public String getCode() {
    return code;
  }
}
