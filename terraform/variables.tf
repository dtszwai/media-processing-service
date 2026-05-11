variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-west-2"
}

variable "is_local" {
  description = "Whether running in LocalStack (skips AWS-only resources)"
  type        = bool
  default     = false
}

variable "localstack_endpoint" {
  description = "LocalStack endpoint URL (only used when is_local=true)"
  type        = string
  default     = "http://localhost:4566"
}

variable "localstack_lambda_endpoint" {
  description = "LocalStack endpoint for Lambda env vars (Docker network)"
  type        = string
  default     = "http://localstack:4566"
}

variable "application_environment" {
  description = "Environment (localstack or aws)"
  type        = string
}

variable "api_port" {
  description = "Port the API listens on"
  type        = number
  default     = 9000
}

variable "media_s3_bucket_name" {
  description = "S3 bucket for media files"
  type        = string
}

variable "media_dynamo_table_name" {
  description = "DynamoDB table for media metadata"
  type        = string
}

variable "desired_task_count" {
  description = "Number of ECS tasks to run"
  type        = number
  default     = 1
}

variable "otel_exporter_endpoint" {
  description = "OpenTelemetry exporter endpoint"
  type        = string
  default     = "http://localhost:4318"
}

variable "enable_snapstart" {
  description = "Enable Lambda SnapStart for faster cold starts (Java 21 runtime)"
  type        = bool
  default     = true
}

variable "webhook_secret" {
  description = "Secret for HMAC-signing webhook payloads"
  type        = string
  default     = ""
  sensitive   = true
}

variable "generation_worker_image_uri" {
  description = "Docker image URI for the generation-worker Lambda (container-image path). See module variable for details."
  type        = string
  default     = ""
}

variable "local_stage_poller_enabled" {
  description = "Skip generation-worker SQS event source mappings so the API's in-process poller consumes the queues. See module variable for details."
  type        = bool
  default     = false
}
