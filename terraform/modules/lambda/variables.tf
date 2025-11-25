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

variable "media_bucket_id" {
  description = "ID of the S3 bucket"
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

variable "process_lambda_sg" {
  description = "Security group for process lambda"
  type        = string
}

variable "manage_lambda_sg" {
  description = "Security group for manage lambda"
  type        = string
}

variable "private_subnet_ids" {
  description = "Private subnet IDs"
  type        = list(string)
}
