# Media Processing Service - Application Code

This directory contains the application source code for the media processing service.

## Modules

| Directory  | Description                              |
| ---------- | ---------------------------------------- |
| `api/`     | Spring Boot REST API (port 9000)         |
| `lambdas/` | AWS Lambda handlers for image processing |
| `common/`  | Shared models and events                 |
| `web/`     | Svelte web application                   |

## API Documentation

Interactive API documentation available at `/swagger-ui.html` when the API is running.

## Media Status Flow

```
PENDING_UPLOAD → PENDING → PROCESSING → COMPLETE
                                    ↘ ERROR
                              DELETING → (deleted)
```

| Status           | Description                                            |
| ---------------- | ------------------------------------------------------ |
| `PENDING_UPLOAD` | Presigned URL created, waiting for client upload to S3 |
| `PENDING`        | Upload complete, queued for processing                 |
| `PROCESSING`     | Lambda is processing the image                         |
| `COMPLETE`       | Processing finished, ready for download                |
| `ERROR`          | Processing failed                                      |
| `DELETING`       | Delete requested, waiting for Lambda                   |

## Processing Flow

### Direct Upload (< 100MB)

1. Client uploads image → API stores in S3, metadata in DynamoDB (`PENDING`)
2. API publishes `media.v1.process` event to SNS
3. Lambda receives event, sets status to `PROCESSING`, processes image
4. Lambda stores result in S3 `resized/` prefix, updates status to `COMPLETE`
5. Client polls status, downloads via presigned URL

### Presigned URL Upload (up to 5GB)

1. Client calls `POST /v1/media/upload/init` → API returns presigned S3 PUT URL (`PENDING_UPLOAD`)
2. Client uploads directly to S3 using presigned URL
3. Client calls `POST /v1/media/{id}/upload/complete` → API verifies and publishes event (`PENDING`)
4. Processing continues as above

### Resize

1. Client requests resize → API sets status to `PENDING`, publishes `media.v1.resize`
2. Lambda processes and updates to `COMPLETE`

### Delete

1. Client requests delete → API sets status to `DELETING`, publishes `media.v1.delete`
2. Lambda deletes files from S3, removes metadata from DynamoDB

## Configuration

| Variable                    | Description                | Default      |
| --------------------------- | -------------------------- | ------------ |
| `APP_PORT`                  | API server port            | 9000         |
| `AWS_REGION`                | AWS region                 | us-west-2    |
| `MEDIA_BUCKET_NAME`         | S3 bucket name             | media-bucket |
| `MEDIA_DYNAMODB_TABLE_NAME` | DynamoDB table             | media        |
| `RATE_LIMITING_ENABLED`     | Enable rate limiting       | true         |
| `RATE_LIMIT_API_RPM`        | API requests per minute    | 100          |
| `RATE_LIMIT_UPLOAD_RPM`     | Upload requests per minute | 10           |

See root `docker-compose.yml` for full configuration.
