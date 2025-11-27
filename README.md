# Distributed Media Processing Service

An event-driven image processing pipeline built with Spring Boot and AWS Lambda, demonstrating decoupled architecture patterns and distributed tracing.

## Overview

This service handles image uploads asynchronously using an event-driven architecture. The REST API accepts uploads and publishes events, while Lambda functions handle CPU-intensive image transformations independently, enabling the system to scale ingestion and processing separately.

## Architecture

![High-Level System Architecture](images/high-level-system-architecture.png)

**Key Design Decisions:**

- **SNS/SQS buffering** decouples high-throughput API ingestion from CPU-intensive image transformation
- **Async processing** allows the API to respond immediately while processing happens in background
- **Status polling** enables clients to track processing state without blocking

## Tech Stack

- Java 21, Spring Boot 3.x
- AWS Lambda (image processing)
- AWS S3 (object storage), DynamoDB (metadata)
- AWS SNS/SQS (event messaging)
- OpenTelemetry (distributed tracing)
- Grafana (visualization)
- LocalStack (local AWS emulation)
- Terraform (infrastructure as code)

## Observability

End-to-end distributed tracing with OpenTelemetry enables cross-service visibility from API to Lambda. Traces propagate through SNS/SQS messages, allowing bottleneck detection across the entire pipeline.

Grafana dashboards visualize:

- P95 latency across services
- Processing throughput and error rates
- Trace waterfall views for debugging

Access Grafana at http://localhost:3000 when running locally.

## Getting Started

### Prerequisites

- Java 21
- Maven 3.9+
- Docker & Docker Compose
- tflocal

### Quick Start

```bash
# Build and start everything (API, Lambda, LocalStack, Grafana)
make local-up

# Stop and clean up
make local-down
```

### Example

```bash
# Upload image
curl -X POST -F "file=@photo.jpg" http://localhost:9000/v1/media/upload

# Check status (returns PENDING -> PROCESSING -> COMPLETE)
curl http://localhost:9000/v1/media/{id}/status

# Download when complete
curl -L http://localhost:9000/v1/media/{id}/download -o result.jpg
```

## Processing Flow

1. Client uploads image → API stores in S3, metadata in DynamoDB (status: `PENDING`)
2. API publishes `media.v1.process` event to SNS
3. Lambda receives event, processes image (resize + watermark)
4. Lambda stores result in S3, updates status to `COMPLETE`
5. Client polls status, downloads via presigned URL
