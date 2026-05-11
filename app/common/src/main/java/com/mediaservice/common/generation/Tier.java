package com.mediaservice.common.generation;

import java.util.Locale;

/**
 * Generation pricing/quota tier. The wire and DynamoDB representations stay lowercase strings
 * ({@code "free"} / {@code "paid"}) so the enum is intentionally a thin convenience over those
 * literals — convert at boundaries via {@link #fromString(String)} and {@link #wireValue()}.
 */
public enum Tier {
  FREE,
  PAID;

  /**
   * Normalise an arbitrary input string to a {@code Tier}. Unrecognised, null, or malformed
   * values fall back to {@link #FREE} to preserve the legacy {@code normalizeTier} semantics.
   */
  public static Tier fromString(String value) {
    if (value == null) {
      return FREE;
    }
    try {
      return Tier.valueOf(value.toUpperCase(Locale.ROOT));
    } catch (IllegalArgumentException e) {
      return FREE;
    }
  }

  /** Lowercase wire/storage representation (e.g. {@code "free"}, {@code "paid"}). */
  public String wireValue() {
    return name().toLowerCase(Locale.ROOT);
  }
}
