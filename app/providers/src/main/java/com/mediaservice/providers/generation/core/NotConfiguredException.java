package com.mediaservice.providers.generation.core;

public class NotConfiguredException extends GenerationProviderException {
  public static final String CODE = "NOT_CONFIGURED";

  public NotConfiguredException(String provider, String requirement) {
    super(CODE, CODE + ": " + provider + " is not configured: missing " + requirement);
  }
}
