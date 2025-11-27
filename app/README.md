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

## Configuration

| Variable                    | Description     | Default      |
| --------------------------- | --------------- | ------------ |
| `APP_PORT`                  | API server port | 9000         |
| `AWS_REGION`                | AWS region      | us-west-2    |
| `MEDIA_BUCKET_NAME`         | S3 bucket name  | media-bucket |
| `MEDIA_DYNAMODB_TABLE_NAME` | DynamoDB table  | media        |

See root `docker-compose.yml` for full configuration.
