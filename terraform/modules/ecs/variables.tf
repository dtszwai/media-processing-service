variable "additional_tags" {
  description = "Additional tags applied to all resources."
  type        = map(string)
  default     = {}
}

variable "vpc_id" {
  description = "VPC id."
  type        = string
}

variable "app_port" {
  description = "Port the api container listens on (must match Go API_HTTP_ADDR)."
  type        = number
}

variable "ecr_repository_arn" {
  description = "ARN of the ECR repository holding the api image."
  type        = string
}

variable "api_image_uri" {
  description = "Pushed api image URI including tag."
  type        = string
}

variable "dynamodb_table_name" {
  description = "DynamoDB table name (matches Go DDB_TABLE)."
  type        = string
}

variable "dynamodb_table_arn" {
  description = "ARN of the DynamoDB table."
  type        = string
}

variable "media_bucket_arn" {
  description = "ARN of the media S3 bucket."
  type        = string
}

variable "media_s3_bucket_name" {
  description = "Media bucket name (matches Go S3_BUCKET)."
  type        = string
}

variable "media_topic_arn" {
  description = "ARN of the media-management SNS topic (matches Go SNS_MEDIA_TOPIC_ARN)."
  type        = string
}

variable "media_topic_name" {
  description = "Name of the media-management SNS topic (matches Go SNS_MEDIA_TOPIC)."
  type        = string
}

variable "media_cleanup_topic_arn" {
  description = "ARN of the media-cleanup SNS topic (matches Go SNS_MEDIA_CLEANUP_TOPIC_ARN)."
  type        = string
}

variable "media_cleanup_topic_name" {
  description = "Name of the media-cleanup SNS topic (matches Go SNS_MEDIA_CLEANUP_TOPIC)."
  type        = string
}

variable "generation_topic_arn" {
  description = "ARN of the generation-jobs SNS topic (matches Go SNS_GENERATION_TOPIC_ARN)."
  type        = string
}

variable "generation_topic_name" {
  description = "Name of the generation-jobs SNS topic (matches Go SNS_GENERATION_TOPIC)."
  type        = string
}

variable "analytics_events_topic_arn" {
  description = "ARN of the analytics-events SNS topic (matches Go SNS_ANALYTICS_TOPIC_ARN)."
  type        = string
}

variable "analytics_events_topic_name" {
  description = "Name of the analytics-events SNS topic (matches Go SNS_ANALYTICS_TOPIC)."
  type        = string
}

variable "kms_prompt_key_id" {
  description = "Key id of the KMS prompt envelope key (matches Go KMS_PROMPT_KEY_ID). Empty in LocalStack — bootstrap self-provisions there."
  type        = string
  default     = ""
}

variable "kms_prompt_key_arn" {
  description = "ARN of the KMS prompt envelope key. Scopes the kms:Encrypt/Decrypt/GenerateDataKey policy on the task role. Empty in LocalStack."
  type        = string
  default     = ""
}

variable "generation_queue_arns" {
  description = "Per-tier × resource-class generation queue ARNs."
  type        = map(string)
}

variable "generation_queue_urls" {
  description = "Per-tier × resource-class generation queue URLs."
  type        = map(string)
}

variable "media_queue_arn" {
  description = "ARN of the media-jobs SQS queue."
  type        = string
}

variable "media_queue_name" {
  description = "Name of the media-jobs SQS queue (matches Go SQS_MEDIA_QUEUE)."
  type        = string
}

variable "media_queue_url" {
  description = "URL of the media-jobs SQS queue (matches Go SQS_MEDIA_QUEUE_URL)."
  type        = string
}

variable "media_cleanup_queue_arn" {
  description = "ARN of the media-cleanup SQS queue."
  type        = string
}

variable "media_cleanup_queue_name" {
  description = "Name of the media-cleanup SQS queue (matches Go SQS_MEDIA_CLEANUP_QUEUE)."
  type        = string
}

variable "media_cleanup_queue_url" {
  description = "URL of the media-cleanup SQS queue (matches Go SQS_MEDIA_CLEANUP_QUEUE_URL)."
  type        = string
}

variable "media_upload_events_queue_arn" {
  description = "ARN of the media-upload-events SQS queue."
  type        = string
}

variable "media_upload_events_queue_name" {
  description = "Name of the media-upload-events SQS queue (matches Go SQS_MEDIA_UPLOAD_EVENTS_QUEUE)."
  type        = string
}

variable "media_upload_events_queue_url" {
  description = "URL of the media-upload-events SQS queue (matches Go SQS_MEDIA_UPLOAD_EVENTS_QUEUE_URL)."
  type        = string
}

variable "webhook_queue_arn" {
  description = "ARN of the webhook-delivery SQS queue."
  type        = string
}

variable "webhook_queue_name" {
  description = "Name of the webhook-delivery SQS queue (matches Go SQS_WEBHOOK_QUEUE)."
  type        = string
}

variable "webhook_queue_url" {
  description = "URL of the webhook-delivery SQS queue (matches Go SQS_WEBHOOK_QUEUE_URL)."
  type        = string
}

variable "analytics_tracker_queue_arn" {
  description = "ARN of the analytics-tracker SQS queue."
  type        = string
}

variable "analytics_tracker_queue_name" {
  description = "Name of the analytics-tracker SQS queue (matches Go SQS_ANALYTICS_QUEUE)."
  type        = string
}

variable "analytics_tracker_queue_url" {
  description = "URL of the analytics-tracker SQS queue (matches Go SQS_ANALYTICS_QUEUE_URL)."
  type        = string
}

variable "public_subnet_ids" {
  description = "Public subnet IDs (ALB)."
  type        = list(string)
}

variable "private_subnet_ids" {
  description = "Private subnet IDs (tasks)."
  type        = list(string)
}

variable "alb_sg_id" {
  description = "Security group attached to the ALB."
  type        = string
}

variable "container_sg_id" {
  description = "Security group attached to api tasks."
  type        = string
}

variable "application_environment" {
  description = "Logical environment value injected as MSG_ENV."
  type        = string
}

variable "otel_exporter_endpoint" {
  description = "OTLP exporter endpoint for the api task."
  type        = string
  default     = ""
}

variable "app_log_group_name" {
  description = "CloudWatch Logs group for api task stdout/stderr."
  type        = string
}

variable "desired_task_count" {
  description = "Number of api tasks."
  type        = number
}

variable "webhook_secret" {
  description = "HMAC secret for outbound webhooks."
  type        = string
  default     = "local-dev-secret-change-me"
  sensitive   = true
}
