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

variable "generation_sqs_queue_arn" {
  description = "ARN of the generation SQS queue (free tier)"
  type        = string
}

variable "generation_paid_sqs_queue_arn" {
  description = "ARN of the generation SQS queue (paid tier). Subscribes to the same SNS topic with a filter policy on tier=paid."
  type        = string
}

variable "generation_topic_arn" {
  description = "ARN of the generation SNS topic"
  type        = string
}

variable "generation_openai_api_key_secret_arn" {
  description = "Secrets Manager ARN for the generation OpenAI API key. Required when is_local = false."
  type        = string
  default     = ""
}

variable "media_table_cmk_arn" {
  description = "Optional KMS CMK ARN encrypting the media DynamoDB table. When set, the generation worker is granted Decrypt / GenerateDataKey on it."
  type        = string
  default     = null
}

variable "generation_secret_cmk_arn" {
  description = "Optional KMS CMK ARN encrypting the OpenAI key Secrets Manager entry. When set, the generation worker is granted Decrypt / GenerateDataKey on it."
  type        = string
  default     = null
}

variable "generation_budget_alert_pct" {
  description = "Percentage of the daily generation budget at which to fire a budget-used alert."
  type        = string
  default     = "80"
}

variable "region" {
  description = "AWS region the Lambda is deployed into. Used to scope IAM resource ARNs."
  type        = string
  default     = "us-west-2"
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

variable "webhook_secret" {
  description = "Secret for HMAC-signing webhook payloads"
  type        = string
  default     = ""
  sensitive   = true
}

variable "generation_worker_image_uri" {
  description = <<-EOT
    Docker image URI for the generation-worker Lambda. When non-empty the
    Lambda is deployed as a container image (package_type=Image) instead of
    a zip JAR. Required for the NotebookLM provider path because the image
    bundles Python + notebooklm-py alongside the JAR. AWS only — LocalStack
    community does not support container-image Lambdas.
  EOT
  type        = string
  default     = ""
}

variable "local_stage_poller_enabled" {
  description = <<-EOT
    When true, skip the generation-worker SQS event source mappings. The
    API container's in-process stage poller becomes the sole consumer of
    the generation-jobs and generation-jobs-paid queues. Use for the
    LocalStack path where the Lambda runtime cannot host Python.
  EOT
  type        = bool
  default     = false
}
