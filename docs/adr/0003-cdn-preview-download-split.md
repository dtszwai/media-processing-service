# ADR-0003: CDN Preview/Download Split Architecture

## Status
Accepted

## Context
We need to add CDN capabilities to serve media efficiently. The initial approach of serving all processed images via CloudFront with signed URLs was questioned because:

1. Signed URLs limit sharing, reducing CDN cache hit rate
2. Original images are only accessed by Lambda during processing
3. The service access pattern is unclear (public vs private media)

## Decision
Split media variants into three tiers with different access patterns:

| Variant | Storage | Access | CDN | Use Case |
|---------|---------|--------|-----|----------|
| `original` | `{mediaId}/original.{ext}` | Lambda only | No | Processing input |
| `processed` | `{mediaId}/processed.{ext}` | S3 signed URL (1hr) | No | Full-quality download |
| `preview` | `{mediaId}/preview.{ext}` | CloudFront public | Yes | Galleries, sharing |

### Preview Generation
- Max width: 800px
- Quality: 60%
- Watermark: Centered, prominent overlay (50% opacity)
- Generated during initial processing (not resize)

### CloudFront Configuration
- Origin: S3 bucket with Origin Access Control (OAC)
- Path pattern: `*/preview.*` only
- TTL: 1 year (immutable content)
- Price class: PriceClass_100 (NA + Europe)

### S3 Cache Headers
Preview uploads include `Cache-Control: public, max-age=31536000` for optimal CDN caching.

## Consequences

### Positive
- Mirrors commercial asset marketplace pattern (Shutterstock, Adobe Stock)
- High CDN cache hit rate for previews (public, no signing)
- Protected full-resolution downloads
- Cost optimization: CDN egress cheaper than S3 for popular items

### Negative
- Additional storage for preview variant
- Processing time increases slightly for preview generation
- Two-tier access model requires client updates

## Alternatives Considered

1. **CDN for all with signed URLs**: Low cache hit rate, complex key management
2. **No CDN**: Higher latency, no edge caching benefits
3. **CDN for all without signing**: Security concern for full-resolution images
