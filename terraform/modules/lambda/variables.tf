variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "dynamodb_table_arn" {
  description = "ARN of the DynamoDB table"
  type        = string
}

variable "dynamodb_table_name" {
  description = "Name of the DynamoDB table"
  type        = string
}

variable "media_bucket_arn" {
  description = "ARN of the S3 bucket"
  type        = string
}

variable "media_s3_bucket_name" {
  description = "S3 bucket name"
  type        = string
}

variable "media_management_sqs_queue_arn" {
  description = "ARN of the SQS queue"
  type        = string
}

variable "lambdas_src_path" {
  description = "Path to lambda source code"
  type        = string
}

variable "otel_exporter_endpoint" {
  description = "OpenTelemetry exporter endpoint"
  type        = string
}

variable "lambda_sg" {
  description = "Security group for lambda"
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet IDs"
  type        = list(string)
}

variable "lambda_architecture" {
  description = "Lambda architecture (x86_64 or arm64)"
  type        = string
  default     = "x86_64"
}

variable "enable_snapstart" {
  description = "Enable Lambda SnapStart for faster cold starts (Java runtime only)"
  type        = bool
  default     = true
}

variable "is_local" {
  description = "Whether running in LocalStack (disables VPC, SnapStart)"
  type        = bool
  default     = false
}

variable "localstack_endpoint" {
  description = "LocalStack endpoint for Lambda environment variables"
  type        = string
  default     = ""
}
