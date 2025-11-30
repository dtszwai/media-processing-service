package com.mediaservice.common.constants;

/**
 * Constants for S3 and DynamoDB storage keys.
 * Centralizes magic strings to prevent typos and ensure consistency between API
 * and Lambda.
 */
public final class StorageConstants {

  private StorageConstants() {
    // Prevent instantiation
  }

  // S3 key prefixes
  public static final String S3_PREFIX_UPLOADS = "uploads";
  public static final String S3_PREFIX_RESIZED = "resized";

  // DynamoDB key patterns
  public static final String DYNAMO_PK_PREFIX = "MEDIA#";
  public static final String DYNAMO_SK_METADATA = "METADATA";
  public static final String DYNAMO_GSI_SK_CREATED_AT = "SK-createdAt-index";
}
