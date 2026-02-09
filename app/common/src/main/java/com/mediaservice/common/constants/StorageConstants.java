package com.mediaservice.common.constants;

/**
 * Constants for S3 and DynamoDB storage keys.
 * Centralizes magic strings to prevent typos and ensure consistency between API
 * and Lambda.
 *
 * <p>S3 Key Structure:
 * <pre>
 * {tenantId}/{mediaId}/assets/{assetId}.{ext}
 * </pre>
 *
 * @see <a href="docs/adr/0001-s3-storage-structure.md">ADR-0001: S3 Storage Structure</a>
 */
public final class StorageConstants {

  private StorageConstants() {
    // Prevent instantiation
  }

  // S3 asset structure
  public static final String S3_ASSET_PREFIX = "assets";

  // Preview generation settings
  public static final int PREVIEW_MAX_WIDTH = 800;
  public static final float PREVIEW_QUALITY = 0.6f;

  // DynamoDB key patterns
  public static final String DYNAMO_PK_PREFIX = "MEDIA#";
  public static final String DYNAMO_SK_MEDIA = "MEDIA";
  public static final String DYNAMO_SK_ASSET_PREFIX = "ASSET#";
  public static final String DYNAMO_SK_JOB_PREFIX = "JOB#";
  public static final String DYNAMO_SK_METADATA = "METADATA";
  public static final String DYNAMO_GSI_SK_CREATED_AT = "SK-createdAt-index";

  // Short URL key patterns
  public static final String DYNAMO_PK_SHORT_URL_PREFIX = "SHORT#";
  public static final String DYNAMO_SK_SHORT_URL_PREFIX = "SHORT#";

  // DynamoDB attribute names
  public static final String DYNAMO_ATTR_ORIGINAL_FILENAME = "originalFilename";
  public static final String DYNAMO_ATTR_TENANT_ID = "tenantId";
  public static final String DYNAMO_ATTR_USER_ID = "userId";
  public static final String DYNAMO_GSI_TENANT_CREATED_AT = "tenantId-createdAt-index";

  /**
   * Build a tenant-scoped S3 key for a media asset.
   *
   * @param tenantId  The tenant ID
   * @param mediaId   The media ID (UUID)
   * @param assetId   The asset ID (UUID)
   * @param extension The file extension including dot (e.g., ".jpeg")
   * @return The S3 key (e.g., "tenant-123/abc-123/assets/asset-456.jpeg")
   */
  public static String buildAssetKey(String tenantId, String mediaId, String assetId, String extension) {
    return tenantId + "/" + mediaId + "/" + S3_ASSET_PREFIX + "/" + assetId + extension;
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
