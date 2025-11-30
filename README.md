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
- Redis (caching, rate limiting, analytics)
- OpenTelemetry (distributed tracing)
- Grafana (visualization)
- LocalStack (local AWS emulation)
- Terraform (infrastructure as code)

## Production Infrastructure

The API includes production-ready features:

| Feature               | Description                                                                                    |
| --------------------- | ---------------------------------------------------------------------------------------------- |
| **Rate Limiting**     | Redis-backed distributed per-IP limits (100 req/min API, 10 req/min uploads)                   |
| **Caching**           | Redis-backed Spring Cache for media metadata, status, and presigned URLs                       |
| **Analytics**         | View counts, leaderboards (day/week/month/all-time), format usage tracking                     |
| **Circuit Breaker**   | Resilience4j protects AWS calls; opens at 50% failure rate                                     |
| **Security Headers**  | HSTS, CSP, X-Frame-Options, X-Content-Type-Options                                             |
| **Request Tracking**  | UUID per request via `X-Request-ID` header, MDC log correlation                                |
| **Health Checks**     | Kubernetes probes at `/actuator/health/liveness` and `/actuator/health/readiness`              |
| **API Documentation** | OpenAPI/Swagger at `/swagger-ui.html`                                                          |
| **Metrics**           | Actuator metrics at `/actuator/metrics`, circuit breaker status at `/actuator/circuitbreakers` |

Configuration via environment variables or `application.yml`.

## Observability

End-to-end distributed tracing with OpenTelemetry enables cross-service visibility from API to Lambda. Traces propagate through SNS/SQS messages, allowing bottleneck detection across the entire pipeline.

Grafana dashboards visualize:

- P95 latency across services
- Processing throughput and error rates
- Trace waterfall views for debugging

Access Grafana at http://localhost:3000 when running locally.
