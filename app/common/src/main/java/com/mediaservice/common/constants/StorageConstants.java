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
 */
public final class StorageConstants {

  private StorageConstants() {
    // Prevent instantiation
  }

  // S3 asset structure
  public static final String S3_ASSET_PREFIX = "assets";

  // Document preview generation settings
  public static final int DOCUMENT_PREVIEW_MAX_WIDTH = 800;
  public static final float DOCUMENT_PREVIEW_QUALITY = 0.6f;

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

  // Generation pipeline key patterns (single-table)
  public static final String DYNAMO_PK_GEN_PREFIX = "GEN#";
  public static final String DYNAMO_SK_GEN_JOB = "JOB";
  public static final String DYNAMO_SK_STAGE_PREFIX = "STAGE#";
  public static final String DYNAMO_SK_ARTIFACT_PREFIX = "ARTIFACT#";
  public static final String DYNAMO_SK_IDEMPOTENCY_PREFIX = "IDEMPOTENCY#";
  public static final String DYNAMO_SK_SAFETY_PREFIX = "SAFETY#";
  public static final String DYNAMO_SK_AUDIT_PREFIX = "AUDIT#";

  // Budget reservation key patterns
  public static final String DYNAMO_PK_BUDGET_PREFIX = "BUDGET#";
  public static final String DYNAMO_SK_BUDGET_RESERVED = "RESERVED";
  public static final String DYNAMO_SK_BUDGET_RESERVED_PREFIX = "RESERVED#";
  public static final String DYNAMO_SK_BUDGET_USED = "USED";

  // Simulator state + control row keys
  public static final String DYNAMO_PK_SIM_PREFIX = "SIM#";
  public static final String DYNAMO_SK_SIM_STATE = "STATE";
  public static final String DYNAMO_PK_GENERATION_CONTROL = "GENERATION#CONTROL";
  public static final String DYNAMO_SK_GEN_CONTROL_SIMULATOR = "SIMULATOR";

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

  public static String buildGenPk(String jobId) {
    return DYNAMO_PK_GEN_PREFIX + jobId;
  }

  public static String buildBudgetPk(String tenantId, String date) {
    return DYNAMO_PK_BUDGET_PREFIX + tenantId + "#" + date;
  }

  public static String buildStageSk(String stage, int attempt) {
    return DYNAMO_SK_STAGE_PREFIX + stage + "#" + attempt;
  }

  public static String buildSafetySk(String stage, String gate, long timestampMillis) {
    return DYNAMO_SK_SAFETY_PREFIX + stage + "#" + gate + "#" + timestampMillis;
  }

  public static String buildAuditSk(String category, String gate, long timestampMillis) {
    return DYNAMO_SK_AUDIT_PREFIX + category + "#" + gate + "#" + timestampMillis;
  }

  public static String buildIdempotencySk(String stage, String operation) {
    return DYNAMO_SK_IDEMPOTENCY_PREFIX + stage + "#" + operation;
  }

  public static String buildArtifactSk(String artifactId) {
    return DYNAMO_SK_ARTIFACT_PREFIX + artifactId;
  }

  public static String buildSimPk(String providerJobId) {
    return DYNAMO_PK_SIM_PREFIX + providerJobId;
  }

  public static String buildReservedJobSk(String jobId) {
    return DYNAMO_SK_BUDGET_RESERVED_PREFIX + jobId;
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
