package com.mediaservice.common.constants;

/**
 * Constants for S3 and DynamoDB storage keys.
 * Centralizes magic strings to prevent typos and ensure consistency between API
 * and Lambda.
 *
 * <p>S3 Key Structure:
 * <pre>
 * {mediaId}/
 *   original.{ext}     - Original uploaded file
 *   processed.{ext}    - Default processed output
 *   thumb_sm.{ext}     - Small thumbnail (future)
 *   thumb_md.{ext}     - Medium thumbnail (future)
 *   thumb_lg.{ext}     - Large thumbnail (future)
 *   resize_{size}.{ext} - Resized variants (future)
 * </pre>
 *
 * @see <a href="docs/adr/0001-s3-storage-structure.md">ADR-0001: S3 Storage Structure</a>
 */
public final class StorageConstants {

  private StorageConstants() {
    // Prevent instantiation
  }

  // S3 variant names (flat structure: {mediaId}/{variant}.{ext})
  public static final String S3_VARIANT_ORIGINAL = "original";
  public static final String S3_VARIANT_PROCESSED = "processed";
  public static final String VARIANT_PREVIEW = "preview";

  // Preview generation settings
  public static final int PREVIEW_MAX_WIDTH = 800;
  public static final float PREVIEW_QUALITY = 0.6f;

  // DynamoDB key patterns
  public static final String DYNAMO_PK_PREFIX = "MEDIA#";
  public static final String DYNAMO_SK_METADATA = "METADATA";
  public static final String DYNAMO_GSI_SK_CREATED_AT = "SK-createdAt-index";

  // DynamoDB attribute names
  public static final String DYNAMO_ATTR_ORIGINAL_FILENAME = "originalFilename";
  public static final String DYNAMO_ATTR_TENANT_ID = "tenantId";
  public static final String DYNAMO_ATTR_USER_ID = "userId";
  public static final String DYNAMO_GSI_TENANT_CREATED_AT = "tenantId-createdAt-index";

  /**
   * Build an S3 key for a media variant (legacy, no tenant).
   *
   * @param mediaId The media ID (UUID)
   * @param variant The variant name (e.g., "original", "processed")
   * @param extension The file extension including dot (e.g., ".jpeg")
   * @return The S3 key (e.g., "abc-123/original.jpeg")
   */
  public static String buildS3Key(String mediaId, String variant, String extension) {
    return mediaId + "/" + variant + extension;
  }

  /**
   * Build a tenant-scoped S3 key for a media variant.
   *
   * @param tenantId  The tenant ID
   * @param mediaId   The media ID (UUID)
   * @param variant   The variant name (e.g., "original", "processed")
   * @param extension The file extension including dot (e.g., ".jpeg")
   * @return The S3 key (e.g., "tenant-123/abc-123/original.jpeg")
   */
  public static String buildS3Key(String tenantId, String mediaId, String variant, String extension) {
    return tenantId + "/" + mediaId + "/" + variant + extension;
  }

  /**
   * Extract file extension from a filename.
   *
   * @param filename The filename (e.g., "photo.jpg")
   * @return The extension including dot (e.g., ".jpg"), or empty string if none
   */
  public static String getFileExtension(String filename) {
    if (filename == null || filename.isEmpty()) {
      return "";
    }
    int lastDot = filename.lastIndexOf('.');
    return (lastDot > 0) ? filename.substring(lastDot) : "";
  }
}
