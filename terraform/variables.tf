variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-west-2"
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
