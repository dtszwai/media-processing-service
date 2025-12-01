# ADR-0001: S3 Storage Structure

## Status

Accepted

## Date

2025-12-01

## Context

The media processing service stores uploaded images and their processed variants in S3. We needed to decide on an optimal key structure that balances:

1. **Scalability** - Efficient S3 partitioning and access patterns
2. **Predictability** - Ability to construct keys without database lookups
3. **Extensibility** - Easy addition of new variants (thumbnails, different sizes, filters)
4. **Operational simplicity** - Easy deletion, listing, and debugging

### Previous Structure

```
uploads/{mediaId}/{originalFilename}
resized/{mediaId}/{originalFilename}
```

This structure had several drawbacks:

- Related files scattered across different prefixes
- Original filename in key caused URL encoding issues and unpredictability
- Adding new variants required new top-level prefixes
- Deleting all files for a media item required knowing multiple prefixes

## Decision

Adopt a **flat, mediaId-first structure** with variant-based naming:

```
{mediaId}/
  original.{ext}     # Original uploaded file
  processed.{ext}    # Default processed output (resized + watermarked)
  thumb_sm.{ext}     # Small thumbnail (future)
  thumb_md.{ext}     # Medium thumbnail (future)
  thumb_lg.{ext}     # Large thumbnail (future)
  resize_{size}.{ext}  # Size variants (future)
  filter_{name}.{ext}  # Filter variants (future)
```

### Key Construction

Keys are constructed predictably using:

```java
key = mediaId + "/" + variant + "." + format
// Example: "abc-123/processed.webp"
```

The original filename is stored in DynamoDB and served via `Content-Disposition` header for downloads.

## Rationale

### Why mediaId-first (not prefix-first)?

| Concern                | mediaId-first                            | prefix-first               |
| ---------------------- | ---------------------------------------- | -------------------------- |
| List variants          | `s3.list(prefix: "{id}/")` - O(variants) | Need multiple prefix scans |
| Delete media           | Single prefix delete                     | Multiple prefix deletes    |
| Add variant            | Just write new file                      | May need new prefix        |
| Partition distribution | UUID ensures good distribution           | Same                       |

### Why not userId-first?

We evaluated `{userId}/media/{mediaId}/...` but decided against it because:

1. **Access patterns** - Most operations are by mediaId, not userId
2. **User association** - Lives in DynamoDB, not S3 path
3. **Simplicity** - One less ID required for every S3 operation
4. **Flexibility** - Can add userId prefix later if needed for multi-tenancy

If multi-tenancy or GDPR bulk deletion becomes critical, we can prefix with userId at that point.

### Why variant names instead of original filename?

1. **Predictable keys** - No need for DB lookup to construct S3 key
2. **No encoding issues** - Variant names are simple ASCII
3. **Cleaner URLs** - Shorter, cleaner presigned URLs
4. **Self-documenting** - `processed.webp` vs `my vacation photo.webp`

## Consequences

### Positive

- **Simpler key construction** - Predictable without DB lookup
- **Easy enumeration** - List all variants with single prefix
- **Clean deletion** - Delete all variants with single prefix operation
- **Future-proof** - New variants slot in naturally
- **Better debugging** - Can navigate S3 by mediaId directly

### Negative

- **Migration required** - Existing data uses old structure (see Migration section)
- **Original filename not in key** - Must use Content-Disposition for downloads

### Neutral

- **Two levels** - `{mediaId}/{variant}.{ext}` is the right balance

## Migration

For existing data in the old structure:

1. New uploads use new structure immediately
2. Old files can remain or be migrated via background job
3. Code handles both structures during transition (not implemented - clean cut)

## Implementation

### Constants

```java
// StorageConstants.java
public static final String S3_VARIANT_ORIGINAL = "original";
public static final String S3_VARIANT_PROCESSED = "processed";

public static String buildS3Key(String mediaId, String variant, String extension) {
    return mediaId + "/" + variant + extension;
}
```

### Files Changed

- `app/common/src/main/java/com/mediaservice/common/constants/StorageConstants.java`
- `app/api/src/main/java/com/mediaservice/media/infrastructure/storage/S3StorageService.java`
- `app/lambdas/src/main/java/com/mediaservice/lambda/service/S3Service.java`
- `app/lambdas/src/main/java/com/mediaservice/lambda/ManageMediaHandler.java`
- All related tests
- `app/web/src/lib/api.ts`

## Future Extensions

When adding new variants:

```java
// Thumbnails
public static final String S3_VARIANT_THUMB_SM = "thumb_sm";   // 128px
public static final String S3_VARIANT_THUMB_MD = "thumb_md";   // 256px
public static final String S3_VARIANT_THUMB_LG = "thumb_lg";   // 512px

// Size variants
public static final String S3_VARIANT_480P = "resize_480p";
public static final String S3_VARIANT_720P = "resize_720p";
public static final String S3_VARIANT_1080P = "resize_1080p";

// Filters
public static final String S3_VARIANT_FILTER_SEPIA = "filter_sepia";
public static final String S3_VARIANT_FILTER_BW = "filter_bw";
```

## References

- [AWS S3 Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance.html)
- [S3 Request Rate Performance](https://docs.aws.amazon.com/AmazonS3/latest/userguide/request-rate-perf-considerations.html)
