# Distributed Media Processing Service

Event-driven media platform written in Go: multi-tenant uploads, async asset derivation, a separate text-to-image / audio-overview generation pipeline, analytics, short links, and operational tooling. Frontend is a ops playground used to exercise backend-owned workflows.

## Architecture

![High-Level System Architecture](images/high-level-system-architecture.png)

- SNS/SQS buffering decouples API ingestion from processing
- Asset-based processing model — multiple outputs per media item
- Soft delete keeps the DDB row for analytics/audit while hard-deleting S3 objects
- OpenTelemetry traces flow API → SNS/SQS → workers end-to-end
- Local AWS services are adapted to LocalStack Community: supported services run in LocalStack, while unsupported production-shape services stay reference-only and local containers cover the runtime path.

## Local Development

Requires: Docker + `make` + `pnpm` + `go` + (for `tf-up`) `tflocal`.

Two-tier mental model. `make up` is enough for a healthy API. `make tf-up` adds the LocalStack-supported Terraform topology + cross-compiled Lambdas — use when you need event-source mappings, scheduler rules, or the fuller AWS-shaped resource set. There is no separate real-AWS apply path.

LocalStack Community does not provide every AWS service used by a production-style design, so the local AWS topology is intentionally adapted: Compose runs the long-lived API and generation worker, Terraform provisions the supported AWS-shaped services, and unsupported modules such as ECS and CloudFront remain disabled reference modules.

```bash
make up            # Regen proto + start compose (LocalStack + Redis + Grafana + API + generation worker), wait for /healthz
make tf-up         # Cross-compile Lambda bootstrap zips + tflocal init/apply against LocalStack
pnpm -C frontend install
pnpm -C frontend dev   # Svelte web app on :3001
```

Service URLs:

- API: http://localhost:9000
- Health: http://localhost:9000/healthz
- LocalStack: http://localhost:4566
- Grafana (Loki + Tempo + Mimir + OTel collector): http://localhost:3000
- OTLP gRPC / HTTP: localhost:4317 / 4318
- Web UI (when running `pnpm -C frontend dev`): http://localhost:3001
