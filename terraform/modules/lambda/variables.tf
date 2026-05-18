variable "additional_tags" {
  description = "Additional tags to apply to all resources."
  type        = map(string)
  default     = {}
}

variable "name_prefix" {
  description = "Prefix applied to function and IAM names."
  type        = string
}

variable "localstack_endpoint" {
  description = "Runtime LocalStack endpoint baked into AWS_ENDPOINT_URL on every function."
  type        = string
}

variable "aws_region" {
  description = "AWS region — propagated into function env."
  type        = string
}

variable "media_s3_bucket_name" {
  description = "Bucket name (matches Go S3_BUCKET env)."
  type        = string
}

variable "media_dynamodb_table_name" {
  description = "DynamoDB table name (matches Go DDB_TABLE env)."
  type        = string
}

variable "media_bucket_arn" {
  description = "ARN of the media bucket."
  type        = string
}

variable "dynamodb_table_arn" {
  description = "ARN of the DynamoDB table."
  type        = string
}

variable "media_topic_arn" {
  description = "ARN of the media-management SNS topic."
  type        = string
}

variable "media_topic_name" {
  description = "Name of the media-management SNS topic (matches Go SNS_MEDIA_TOPIC env)."
  type        = string
}

variable "generation_topic_arn" {
  description = "ARN of the generation-jobs SNS topic."
  type        = string
}

variable "generation_topic_name" {
  description = "Name of the generation-jobs SNS topic (matches Go SNS_GENERATION_TOPIC env)."
  type        = string
}

variable "media_queue_arn" {
  description = "ARN of the media-jobs SQS queue."
  type        = string
}

variable "media_queue_url" {
  description = "URL of the media-jobs SQS queue."
  type        = string
}

variable "media_queue_name" {
  description = "Name of the media-jobs SQS queue (matches Go SQS_MEDIA_QUEUE env)."
  type        = string
}

variable "generation_queue_arns" {
  description = "Per-tier × resource-class generation queue ARNs keyed by lowercase tier-class."
  type        = map(string)
}

variable "generation_dlq_arns" {
  description = "Per-tier × resource-class generation DLQ ARNs keyed by lowercase tier-class."
  type        = map(string)
}

variable "generation_queue_urls" {
  description = "Per-tier × resource-class generation queue URLs keyed by lowercase tier-class."
  type        = map(string)
}

variable "webhook_queue_arn" {
  description = "ARN of the webhook-delivery SQS queue."
  type        = string
}

variable "webhook_queue_url" {
  description = "URL of the webhook-delivery SQS queue."
  type        = string
}

variable "webhook_queue_name" {
  description = "Name of the webhook-delivery SQS queue (matches Go SQS_WEBHOOK_QUEUE env)."
  type        = string
}

variable "media_cleanup_queue_arn" {
  description = "ARN of the media-cleanup SQS queue."
  type        = string
}

variable "media_cleanup_queue_name" {
  description = "Name of the media-cleanup SQS queue (matches Go SQS_MEDIA_CLEANUP_QUEUE env)."
  type        = string
}

variable "media_cleanup_queue_url" {
  description = "URL of the media-cleanup SQS queue (matches Go SQS_MEDIA_CLEANUP_QUEUE_URL env)."
  type        = string
}

variable "media_cleanup_topic_arn" {
  description = "ARN of the media-cleanup SNS topic (matches Go SNS_MEDIA_CLEANUP_TOPIC_ARN env)."
  type        = string
}

variable "media_cleanup_topic_name" {
  description = "Name of the media-cleanup SNS topic (matches Go SNS_MEDIA_CLEANUP_TOPIC env)."
  type        = string
}

variable "media_upload_events_queue_arn" {
  description = "ARN of the media-upload-events SQS queue — event source for the upload-events-worker Lambda."
  type        = string
}

variable "media_upload_events_queue_name" {
  description = "Name of the media-upload-events SQS queue (matches Go SQS_MEDIA_UPLOAD_EVENTS_QUEUE env)."
  type        = string
}

variable "media_upload_events_queue_url" {
  description = "URL of the media-upload-events SQS queue (matches Go SQS_MEDIA_UPLOAD_EVENTS_QUEUE_URL env)."
  type        = string
}

variable "otel_exporter_endpoint" {
  description = "OTLP endpoint Lambdas publish traces to."
  type        = string
  default     = ""
}

variable "webhook_secret" {
  description = "HMAC secret for outbound webhooks."
  type        = string
  default     = "local-dev-secret-change-me"
  sensitive   = true
}

variable "analytics_events_topic_arn" {
  description = "ARN of the analytics-events SNS topic (matches Go SNS_ANALYTICS_TOPIC_ARN env)."
  type        = string
}

variable "analytics_events_topic_name" {
  description = "Name of the analytics-events SNS topic (matches Go SNS_ANALYTICS_TOPIC env)."
  type        = string
}

variable "analytics_tracker_queue_arn" {
  description = "ARN of the analytics-tracker SQS queue — event source for the analytics-worker Lambda."
  type        = string
}

variable "analytics_tracker_queue_name" {
  description = "Name of the analytics-tracker SQS queue (matches Go SQS_ANALYTICS_QUEUE env)."
  type        = string
}

variable "analytics_tracker_queue_url" {
  description = "URL of the analytics-tracker SQS queue (matches Go SQS_ANALYTICS_QUEUE_URL env)."
  type        = string
}

variable "lease_reaper_tenants" {
  # Default is empty so the Lambda boots safely without scanning anything.
  description = "Comma-separated tenant IDs the lease-reaper scans on each cron invocation. Empty disables reaping."
  type        = string
  default     = ""
}

variable "kms_prompt_key_id" {
  description = "Key id of the KMS prompt envelope key (matches Go KMS_PROMPT_KEY_ID)."
  type        = string
}

variable "kms_prompt_key_arn" {
  description = "ARN of the KMS prompt envelope key. Scopes the kms:Encrypt/Decrypt/GenerateDataKey policy on the lambda common role."
  type        = string
}
