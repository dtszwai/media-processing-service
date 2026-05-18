# Variables for the LocalStack-targeted Terraform configuration.
#
# This config has one apply target (LocalStack Community via tflocal). Modules
# that LocalStack Community cannot run are gated with `count = 0` at their call
# sites in main.tf, with a comment explaining why.

variable "aws_region" {
  description = "AWS region for the deployment."
  type        = string
  default     = "us-east-1"
}

variable "localstack_provider_endpoint" {
  description = "LocalStack endpoint Terraform uses from the host."
  type        = string
  default     = "http://localhost:4566"
}

variable "localstack_runtime_endpoint" {
  description = "LocalStack endpoint used by Lambda and container runtimes."
  type        = string
  default     = "http://localstack:4566"
}

variable "name_prefix" {
  description = "Prefix applied to SNS/SQS/Lambda/IAM resource names."
  type        = string
  default     = "media-service-go-local"
}

variable "api_port" {
  description = "TCP port the api container listens on. Must match cmd/api API_HTTP_ADDR."
  type        = number
  default     = 9000
}

variable "media_s3_bucket_name" {
  description = "S3 bucket holding media + assets. Must match the value the api reads via S3_BUCKET."
  type        = string
  default     = "media-service-local"
}

variable "media_dynamodb_table_name" {
  description = "DynamoDB single-table name. Must match the value the api reads via DDB_TABLE."
  type        = string
  default     = "media-v1"
}

variable "otel_exporter_endpoint" {
  description = "OTLP gRPC endpoint Lambdas publish traces to (Grafana otel-lgtm container)."
  type        = string
  default     = "http://grafana:4317"
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
  default = {
    App = "media-service"
  }
}

variable "lease_reaper_tenants" {
  # Default is empty so the lease-reaper Lambda scans nothing until operators
  # explicitly list the tenant IDs they want reaped on each 5-minute cron.
  description = "Comma-separated tenant IDs the lease-reaper Lambda scans on each cron invocation. Empty disables reaping."
  type        = string
  default     = ""
}
