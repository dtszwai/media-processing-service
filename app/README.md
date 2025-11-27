# Media Processing Service - Application Code

This directory contains the application source code.

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

## Media Status

| Status     | Description                             |
| ---------- | --------------------------------------- |
| PENDING    | Upload complete, waiting for processing |
| PROCESSING | Lambda is processing the image          |
| COMPLETE   | Processing finished, ready for download |
| ERROR      | Processing failed                       |
| DELETING   | Delete requested, waiting for Lambda    |

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
