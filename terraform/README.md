# terraform/ — LocalStack-targeted infrastructure

Single apply target: **LocalStack Community** via `tflocal apply` (or `make tf-up` from the repo root, which also cross-compiles the Lambda bootstrap zips).

There is no second backend, no local/prod branching toggle, no `prod.tfvars`. Modules that LocalStack Community does not provide stay in source only when they document a concrete AWS-shape deployment gap, and their root-level `module {}` calls are gated `count = 0` with a comment explaining why.

## Modules

| Module | Scope | Active here? |
|--------|-------|--------------|
| `modules/networking` | VPC, public/private subnets, route tables, NAT | ✅ (LocalStack mocks at API level) |
| `modules/s3` | `aws_s3_bucket` with versioning, CORS, and S3 ObjectCreated notification wiring. | ✅ |
| `modules/dynamodb` | `media-v1` single-table with GSIs for jobs, tenant media, leases, lifecycle, audit, and asset-role lookup. TTL uses `ttl_epoch`; PITR enabled. | ✅ |
| `modules/sns-sqs` | Media, cleanup, generation, and analytics topics; direct webhook and S3 upload-event queues; per-tier × resource-class generation queues; DLQs and queue alarms. | ✅ |
| `modules/kms` | Prompt envelope key (AES-256-GCM data-key wrapper). | ✅ |
| `modules/lambda` | Go bootstrap zips on `provided.al2023` (arm64) + IAM + SQS event-source mappings + Scheduler crons. Generation queue mappings are disabled locally. | ✅ |
| `modules/ecs` | Fargate cluster + task def for `cmd/api`, ALB target groups, autoscaling. | ❌ `count = 0` — LocalStack Community does not support ECS. The api runs as a docker compose service. |
| `modules/cloudfront` | Distribution in front of the S3 media bucket. | ❌ `count = 0` — LocalStack Community does not support CloudFront. |

## Generation queue ESMs

The generation-queue Lambda event-source-mappings are deliberately not created (`for_each = {}` in `modules/lambda/main.tf`). LocalStack Community's SQS ESM pump lags badly, which starves the generation FSM. The compose `generation-worker` drains those queues directly by long polling and is the only supported local drainer; solo-API local setups (compose `api` without the worker) are not supported.

To exercise the AWS-shape ESM path against real AWS or LocalStack Pro, edit the `aws_lambda_event_source_mapping.generation` resource to use `var.generation_queue_arns` instead of `{}`.

## Lambda packaging

`make tf-up` cross-compiles each Lambda artifact from `backend/cmd/workers/*` and `backend/cmd/cron/*`:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go -C backend build -tags lambda.norpc -o ../backend/dist/lambda/<artifact>/bootstrap ./cmd/<source>
```

Then `tflocal init && tflocal apply`. `modules/lambda` zips each `bootstrap` and wires the SQS triggers + Scheduler crons.

## Applying

```bash
cd terraform

# Validate (offline) — useful in CI.
terraform init -backend=false
terraform validate

# Apply against LocalStack (or `make tf-up` from repo root).
tflocal init
tflocal apply -auto-approve
```

## Variables

See `variables.tf`. All have sensible defaults; none need to be overridden in normal use.

| Variable | Default | Purpose |
|----------|---------|---------|
| `aws_region` | `us-east-1` | Region (mocked) |
| `name_prefix` | `media-service-go-local` | Resource name prefix |
| `api_port` | `9000` | API HTTP port |
| `media_s3_bucket_name` | `media-service-local` | Media bucket name |
| `media_dynamodb_table_name` | `media-v1` | DynamoDB table name |
| `otel_exporter_endpoint` | `http://grafana:4317` | OTLP gRPC endpoint |
| `lease_reaper_tenants` | `""` | Comma-separated tenant IDs the lease-reaper scans (empty = no-op) |
| `tags` | `{ App = "media-service" }` | Common tags |
