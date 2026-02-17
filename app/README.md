# Media Processing Service - Application Code

This directory contains the application source code for the media processing service.

## Modules

| Directory  | Description                              |
| ---------- | ---------------------------------------- |
| `api/`     | Spring Boot REST API (port 9000)         |
| `lambdas/` | AWS Lambda handlers for media processing |
| `common/`  | Shared models and events                 |
| `web/`     | Svelte web application                   |

## API Documentation

Interactive API documentation is available at `/swagger-ui.html` when the API is running.

## Domain Model

### Media Status

| Status | Meaning |
| --- | --- |
| `PENDING_UPLOAD` | Presigned upload initialized; waiting for client to upload to S3 |
| `PENDING` | Reserved/legacy transitional state (may exist on older records) |
| `PROCESSING` | One or more derived assets are processing |
| `COMPLETE` | All required assets are complete |
| `ERROR` | At least one processing operation failed |
| `DELETED` | Soft-deleted record retained for TTL/analytics; S3 cleanup happens asynchronously |

### Asset Status

| Status | Meaning |
| --- | --- |
| `PENDING_UPLOAD` | Original asset awaiting presigned upload completion |
| `PENDING` | Asset job queued |
| `PROCESSING` | Lambda is processing this asset |
| `COMPLETE` | Asset available for download |
| `ERROR` | Asset processing failed |
| `DELETED` | Asset is no longer active |

### Supported Asset Operations

| Operation | Media Type | Output |
| --- | --- | --- |
| `image.process` | `image` | Resized/converted derivative (JPEG/PNG/WebP) |
| `image.thumbnail` | `image` | Small preview image |
| `document.preview` | `document` | First-page PNG preview |
| `document.text` | `document` | Extracted text as JSON |

## Processing Flows

### Upload and Derive Assets

1. Upload original media:
   - Direct: `POST /v1/media/upload` (<= 50MB)
   - Presigned: `POST /v1/media/upload/init` -> upload to S3 -> `POST /v1/media/{id}/upload/complete` (<= 1GB)
2. API stores media + original asset metadata in DynamoDB.
3. Client requests derivatives via `POST /v1/media/{id}/assets`.
4. API creates processing jobs and publishes events.
5. Lambda updates asset/media status and persists results.

### Document-Specific Processing

- Validates PDF magic bytes, encryption, and max pages (default 200)
- Generates preview PNG (`document.preview`)
- Extracts per-page text payload (`document.text`)
- Stores document metadata on media record (title, author, page count, text stats)

### Delete Flow

1. `DELETE /v1/media/{id}` marks media as `DELETED` with retention TTL.
2. Delete event is published.
3. Lambda removes associated S3 objects asynchronously.

## Where To Find Details

- Endpoint-level API details: OpenAPI/Swagger (`/swagger-ui.html`)
- Runtime/service configuration: `/app/api/src/main/resources/application.yml`
- Local environment defaults: `/docker-compose.yml`
