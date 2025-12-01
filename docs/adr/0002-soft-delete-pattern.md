# ADR-0002: Soft Delete Pattern for Media

## Status

Accepted

## Date

2025-12-01

## Context

When a user deletes media, we need to decide what happens to:

1. **Media record** in DynamoDB
2. **S3 files** (original and processed)
3. **Analytics data** (views, downloads, format usage)
4. **Cache entries** (L1/L2/Redis)

The challenge is that analytics data references media by ID. If we hard delete the media record, analytics queries return "Unknown" for deleted media, creating orphaned data and confusing leaderboards.

### Requirements

- Analytics should remain accurate (historical view counts preserved)
- Deleted media should not appear in media listings
- Analytics leaderboards should handle deleted media gracefully
- Storage costs should not grow indefinitely
- System should support future "restore" functionality

### Options Considered

| Option                                  | Description                          | Pros                   | Cons                                       |
| --------------------------------------- | ------------------------------------ | ---------------------- | ------------------------------------------ |
| **Hard delete everything**              | Delete media, analytics, S3          | Clean slate            | Loses historical accuracy, complex cascade |
| **Hard delete with orphaned analytics** | Delete media, keep analytics         | Simple                 | "Unknown" in leaderboards, confusing UX    |
| **Separate deleted tracking**           | Delete media, track IDs in Redis set | No schema change       | Loses metadata, two systems                |
| **Soft delete on entity**               | Mark as DELETED, keep record         | Full context preserved | Table grows (mitigated by TTL)             |

## Decision

Implement **soft delete on the Media entity** with the following approach:

### Data Model

```java
public enum MediaStatus {
    PENDING_UPLOAD,  // Presigned URL generated, awaiting upload
    PENDING,         // Uploaded, awaiting processing
    PROCESSING,      // Lambda is processing
    COMPLETE,        // Successfully processed
    ERROR,           // Processing failed
    DELETED          // Soft deleted (NEW)
}

public class Media {
    // ... existing fields ...
    private Instant deletedAt;  // null = active, set = deleted (NEW)
}
```

### Delete Flow

```
1. API: Validate media exists and is not already DELETED
2. API: Update status → DELETED, set deletedAt = now()
3. API: Publish DELETE_MEDIA event to SNS
4. API: Invalidate all caches (L1, L2, Redis, Pub/Sub)
5. Lambda: Delete S3 files (original + processed)
6. Lambda: Do NOT delete DynamoDB record
7. (Future) TTL: DynamoDB auto-deletes record after 90 days
```

### Query Behavior

| Endpoint                | Behavior                                        |
| ----------------------- | ----------------------------------------------- |
| `GET /v1/media` (list)  | Filter out DELETED status                       |
| `GET /v1/media/{id}`    | Return 410 Gone for DELETED                     |
| `DELETE /v1/media/{id}` | Idempotent, 202 if exists, 404 if never existed |
| Analytics endpoints     | Include deleted media with metadata             |

### Analytics Display (UI/UX)

Deleted media in analytics leaderboards:

- Show original filename (preserved in record)
- Visual indicator: muted/greyed styling with "Deleted" badge
- Tooltip: "This media was deleted on {date}"
- Click behavior: Show info card (no preview available)
- Optional filter: "Hide deleted items" toggle

## Rationale

### Why soft delete on entity?

1. **Single source of truth** - No separate tracking system
2. **Metadata preserved** - Analytics can show filename, not "Unknown"
3. **Audit trail** - `deletedAt` timestamp for compliance
4. **Future restore** - Can implement "undelete" feature
5. **Industry standard** - Used by GitHub, Slack, AWS, most SaaS

### Why still delete S3 files?

1. **Cost savings** - S3 storage is the main cost driver
2. **No recovery needed** - Users can re-upload if needed
3. **Security** - Files are truly gone when deleted
4. **Simplicity** - No lifecycle rules or trash management

### Why DELETED status (not just deletedAt)?

1. **Query efficiency** - GSI on status for efficient filtering
2. **State machine clarity** - Explicit terminal state
3. **Consistency** - Matches existing status pattern
4. **Simple predicates** - `status != DELETED` vs `deletedAt IS NULL`

### Why 90-day retention before hard delete?

1. **Recovery window** - Allows manual recovery if needed
2. **Analytics stability** - Leaderboards remain stable
3. **Compliance** - Audit trail preserved
4. **Cost negligible** - DynamoDB records are small (~1KB)

## Consequences

### Positive

- **Analytics accuracy** - Historical views preserved, names resolved
- **Clean UX** - No "Unknown" entries in leaderboards
- **Extensible** - Easy to add restore functionality
- **Debuggable** - Can see what was deleted and when
- **Storage efficient** - S3 files deleted immediately

### Negative

- **DynamoDB growth** - Soft-deleted records accumulate (mitigated by TTL)
- **Query overhead** - Must filter DELETED in listings (mitigated by GSI)
- **Complexity** - Status field has more states to handle

### Neutral

- **No breaking API changes** - DELETE returns same 202
- **Backward compatible** - Existing clients unaffected

## Implementation

### Files Changed

**Common Module:**

- `MediaStatus.java` - Add DELETED enum value
- `Media.java` - Add deletedAt field

**API Module:**

- `MediaApplicationService.java` - Soft delete logic
- `MediaDynamoDbRepository.java` - Filter DELETED in listings
- `MediaController.java` - Return 410 Gone for deleted
- `AnalyticsService.java` - Include deleted media in leaderboards

**Lambda Module:**

- `ManageMediaHandler.java` - Only delete S3, not DynamoDB

**Web App:**

- `TopMediaTable.svelte` - Deleted item styling
- `MediaPreview.svelte` - Handle deleted state
- Types and API updates

### DynamoDB Attribute

```
deletedAt: String (ISO-8601 timestamp, null for active records)
```

### Future Enhancements

1. **TTL-based hard delete** - Add `ttl` attribute for auto-expiry
2. **Restore endpoint** - `POST /v1/media/{id}/restore`
3. **Admin purge** - Force hard delete for compliance
4. **Retention policies** - Configurable per-user or per-tier

## References

- [Soft Delete Pattern](https://docs.microsoft.com/en-us/azure/architecture/patterns/soft-delete)
- [AWS DynamoDB TTL](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/TTL.html)
- [GitHub's Soft Delete](https://github.blog/2021-05-10-soft-delete-for-github-repositories/)
