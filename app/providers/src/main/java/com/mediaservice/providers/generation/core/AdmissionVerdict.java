package com.mediaservice.providers.generation.core;

import java.util.Map;

public record AdmissionVerdict(
    boolean allowed,
    Decision decision,
    String code,
    String message,
    int retryAfterSeconds,
    Map<String, String> metadata
) {
  public enum Decision {
    ACCEPTED,
    ACCEPTED_DELAYED,
    DEGRADED,
    REJECTED
  }

  public static AdmissionVerdict allow() {
    return new AdmissionVerdict(true, Decision.ACCEPTED, "ADMITTED", "Admitted", 0, Map.of());
  }

  public static AdmissionVerdict allow(Map<String, String> metadata) {
    return new AdmissionVerdict(true, Decision.ACCEPTED, "ADMITTED", "Admitted", 0, metadata);
  }

  public static AdmissionVerdict acceptedDelayed(String code, String message, int retryAfterSeconds,
      Map<String, String> metadata) {
    return new AdmissionVerdict(true, Decision.ACCEPTED_DELAYED, code, message, retryAfterSeconds, metadata);
  }

  public static AdmissionVerdict degraded(String code, String message, int retryAfterSeconds,
      Map<String, String> metadata) {
    return new AdmissionVerdict(true, Decision.DEGRADED, code, message, retryAfterSeconds, metadata);
  }

  public static AdmissionVerdict reject(String code, String message, int retryAfterSeconds) {
    return new AdmissionVerdict(false, Decision.REJECTED, code, message, retryAfterSeconds, Map.of());
  }
}
