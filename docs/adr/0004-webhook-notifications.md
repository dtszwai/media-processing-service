# ADR-0004: Webhook Notifications for Processing Completion

## Status

Accepted

## Context

Clients need to be notified when media processing completes without continuously polling the status endpoint. The current polling approach has several drawbacks:

1. **Inefficient resource usage** - Clients waste bandwidth and server resources with repeated status checks
2. **Delayed awareness** - Polling intervals mean clients learn about completion with a delay
3. **Scalability concerns** - High polling frequency from many clients can overwhelm the API

We need a push-based notification mechanism that:
- Notifies clients immediately when processing completes
- Is secure and verifiable (clients can confirm notifications are authentic)
- Is optional and backwards-compatible (polling still works)
- Is resilient to temporary client unavailability

## Decision

We implement **optional webhook notifications** with HMAC-SHA256 signatures:

### Webhook Registration

Clients provide an optional `webhookUrl` when initiating uploads:

```json
POST /v1/media/upload/init
{
  "fileName": "photo.jpg",
  "fileSize": 1024000,
  "contentType": "image/jpeg",
  "webhookUrl": "https://api.example.com/webhooks/media"
}
```

Requirements:
- URL must use HTTPS (enforced by validation)
- URL is stored with the media record in DynamoDB
- URL is optional - if not provided, client uses polling

### Webhook Payload

When processing completes, the Lambda sends:

```json
POST https://api.example.com/webhooks/media
Content-Type: application/json
X-Webhook-Signature: <base64-hmac-sha256>
X-Webhook-Timestamp: <unix-timestamp>

{
  "event": "media.processing.complete",
  "mediaId": "abc-123",
  "status": "COMPLETE",
  "fileName": "photo.jpg",
  "mimeType": "image/jpeg",
  "width": 800,
  "outputFormat": "jpeg",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Security: HMAC Signature

Webhooks are signed with HMAC-SHA256 using a shared secret:

```
signature = base64(hmac_sha256(webhook_secret, request_body))
```

Clients verify by:
1. Computing HMAC-SHA256 of the request body with their copy of the secret
2. Comparing with the `X-Webhook-Signature` header
3. Optionally checking `X-Webhook-Timestamp` to prevent replay attacks

### Delivery Guarantees

- **Best-effort delivery**: Webhook failures don't affect processing status
- **Retry with backoff**: 3 attempts with exponential backoff (1s, 2s, 4s)
- **No retry on 4xx**: Client errors (validation failures) are not retried
- **30-second timeout**: Per-request timeout to prevent Lambda timeouts

### Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   API Server    │────▶│     Lambda      │────▶│  Client Server  │
│  (stores URL)   │     │  (sends hook)   │     │  (receives)     │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │                       │                       │
        │  1. Store webhookUrl  │                       │
        │     in DynamoDB       │                       │
        │                       │                       │
        │                       │  2. Process media     │
        │                       │                       │
        │                       │  3. Set COMPLETE      │
        │                       │                       │
        │                       │  4. POST to webhook   │
        │                       │─────────────────────▶│
        │                       │                       │
        │                       │  5. Return 200 OK     │
        │                       │◀─────────────────────│
```

## Alternatives Considered

### 1. WebSocket Connections
- **Pro**: Real-time bidirectional communication
- **Con**: Requires persistent connections, complex state management
- **Con**: Not suitable for serverless architecture

### 2. Server-Sent Events (SSE)
- **Pro**: Simpler than WebSockets, unidirectional
- **Con**: Still requires persistent connections
- **Con**: Not suitable for Lambda-based processing

### 3. SNS/SQS Client Subscription
- **Pro**: AWS-native, highly reliable
- **Con**: Requires clients to set up AWS infrastructure
- **Con**: Adds complexity for simple use cases

### 4. Polling with Long-Polling
- **Pro**: Works with existing infrastructure
- **Con**: Still inefficient compared to push notifications
- **Con**: Adds server load for connection management

## Consequences

### Positive

1. **Immediate notification** - Clients learn about completion instantly
2. **Resource efficient** - No repeated polling requests
3. **Secure** - HMAC signatures prevent spoofing
4. **Optional** - Backwards compatible with polling
5. **Serverless friendly** - Works well with Lambda architecture

### Negative

1. **Client implementation required** - Clients must implement webhook endpoint
2. **Firewall considerations** - Client servers must accept incoming requests
3. **Delivery not guaranteed** - Network issues may cause missed notifications
4. **Secret management** - Shared secret must be securely distributed

### Mitigations

1. **Polling fallback** - Clients can always poll as backup
2. **Idempotent handlers** - Clients should handle duplicate notifications
3. **Status endpoint** - Clients can verify status after receiving webhook

## Configuration

### Lambda Environment Variables

```hcl
WEBHOOK_SECRET = "<shared-secret-for-hmac>"
```

### Client Verification Example (Node.js)

```javascript
const crypto = require('crypto');

function verifyWebhook(body, signature, secret) {
  const computed = crypto
    .createHmac('sha256', secret)
    .update(body)
    .digest('base64');
  return crypto.timingSafeEqual(
    Buffer.from(signature),
    Buffer.from(computed)
  );
}
```
