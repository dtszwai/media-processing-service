# Media Processing Service - Application Code

This directory contains the application source code.

## Structure

```
├── api/                  # Spring Boot REST API
│   ├── src/main/java/
│   │   └── com/mediaservice/
│   │       ├── config/       # AWS, OpenTelemetry config
│   │       ├── controller/   # REST controllers
│   │       ├── service/      # Business logic
│   │       ├── model/        # Domain models
│   │       └── dto/          # Request/Response DTOs
│   └── pom.xml
├── lambdas/              # AWS Lambda functions
│   ├── src/main/java/
│   │   └── com/mediaservice/lambda/
│   │       ├── ManageMediaHandler.java
│   │       ├── config/
│   │       ├── model/
│   │       └── service/
│   └── pom.xml
└── docs/                 # API specs, schemas
```

## API Endpoints

| Method | Endpoint                  | Description              |
| ------ | ------------------------- | ------------------------ |
| GET    | `/v1/media/health`        | Health check             |
| GET    | `/v1/media`               | List all media           |
| POST   | `/v1/media/upload`        | Upload an image          |
| GET    | `/v1/media/{id}`          | Get media metadata       |
| GET    | `/v1/media/{id}/status`   | Get processing status    |
| GET    | `/v1/media/{id}/download` | Download processed image |
| PUT    | `/v1/media/{id}/resize`   | Resize an existing image |
| DELETE | `/v1/media/{id}`          | Delete media             |

## Processing Flow

1. Client uploads image via REST API
2. API stores image in S3 and metadata in DynamoDB (status: `PENDING`)
3. API publishes SNS event for async processing
4. Lambda processes image (resize, watermark) and updates status to `COMPLETE`
5. Client polls status endpoint until complete
6. Client downloads processed image via presigned S3 URL

## Media Status

| Status     | Description                                |
| ---------- | ------------------------------------------ |
| PENDING    | Upload complete, waiting for processing    |
| PROCESSING | Lambda is processing the image             |
| COMPLETE   | Processing finished, ready for download    |
| ERROR      | Processing failed                          |
| DELETING   | Delete requested, waiting for Lambda       |

## Media Response

```json
{
  "mediaId": "uuid",
  "name": "image.jpg",
  "size": 102400,
  "mimetype": "image/jpeg",
  "status": "COMPLETE",
  "width": 500,
  "createdAt": "2025-01-15T10:30:00Z",
  "updatedAt": "2025-01-15T10:30:05Z"
}
```

- `createdAt`: When the media was uploaded (ISO 8601)
- `updatedAt`: When the record was last modified (ISO 8601)

## Configuration

| Variable                    | Description     | Default      |
| --------------------------- | --------------- | ------------ |
| `APP_PORT`                  | API server port | 9000         |
| `AWS_REGION`                | AWS region      | us-west-2    |
| `MEDIA_BUCKET_NAME`         | S3 bucket name  | media-bucket |
| `MEDIA_DYNAMODB_TABLE_NAME` | DynamoDB table  | media        |

See root `docker-compose.yml` for full configuration.
