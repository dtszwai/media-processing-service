output "media_topic_arn" {
  description = "ARN of the media-management SNS topic."
  value       = aws_sns_topic.media.arn
}

output "media_topic_name" {
  description = "Name of the media-management SNS topic (matches Go SNS_MEDIA_TOPIC)."
  value       = aws_sns_topic.media.name
}

output "media_queue_arn" {
  value = aws_sqs_queue.media.arn
}

output "media_queue_url" {
  value = aws_sqs_queue.media.id
}

output "media_queue_name" {
  description = "Name of the media-jobs SQS queue (matches Go SQS_MEDIA_QUEUE)."
  value       = aws_sqs_queue.media.name
}

output "media_dlq_arn" {
  value = aws_sqs_queue.media_dlq.arn
}

output "media_dlq_url" {
  value = aws_sqs_queue.media_dlq.id
}

output "media_cleanup_topic_arn" {
  description = "ARN of the media-cleanup SNS topic."
  value       = aws_sns_topic.media_cleanup.arn
}

output "media_cleanup_topic_name" {
  description = "Name of the media-cleanup SNS topic (matches Go SNS_MEDIA_CLEANUP_TOPIC)."
  value       = aws_sns_topic.media_cleanup.name
}

output "media_cleanup_queue_arn" {
  value = aws_sqs_queue.media_cleanup.arn
}

output "media_cleanup_queue_url" {
  value = aws_sqs_queue.media_cleanup.id
}

output "media_cleanup_queue_name" {
  description = "Name of the media-cleanup SQS queue (matches Go SQS_MEDIA_CLEANUP_QUEUE)."
  value       = aws_sqs_queue.media_cleanup.name
}

output "generation_topic_arn" {
  description = "ARN of the generation-jobs SNS topic."
  value       = aws_sns_topic.generation.arn
}

output "generation_topic_name" {
  description = "Name of the generation-jobs SNS topic (matches Go SNS_GENERATION_TOPIC)."
  value       = aws_sns_topic.generation.name
}

output "generation_queue_arns" {
  description = "Per-tier × resource-class generation queue ARNs keyed by lowercase tier-class."
  value       = { for k, v in aws_sqs_queue.generation : k => v.arn }
}

output "generation_queue_urls" {
  description = "Per-tier × resource-class generation queue URLs keyed by lowercase tier-class."
  value       = { for k, v in aws_sqs_queue.generation : k => v.id }
}

output "generation_dlq_arns" {
  value = { for k, v in aws_sqs_queue.generation_dlq : k => v.arn }
}

output "media_upload_events_queue_arn" {
  description = "ARN of the media-upload-events SQS queue — destination for S3 ObjectCreated notifications."
  value       = aws_sqs_queue.media_upload_events.arn
}

output "media_upload_events_queue_url" {
  description = "URL of the media-upload-events SQS queue (matches Go SQS_MEDIA_UPLOAD_EVENTS_QUEUE)."
  value       = aws_sqs_queue.media_upload_events.id
}

output "media_upload_events_queue_name" {
  description = "Name of the media-upload-events SQS queue."
  value       = aws_sqs_queue.media_upload_events.name
}

output "media_upload_events_dlq_arn" {
  value = aws_sqs_queue.media_upload_events_dlq.arn
}

output "webhook_queue_arn" {
  value = aws_sqs_queue.webhook.arn
}

output "webhook_queue_url" {
  value = aws_sqs_queue.webhook.id
}

output "webhook_queue_name" {
  description = "Name of the webhook-delivery SQS queue (matches Go SQS_WEBHOOK_QUEUE)."
  value       = aws_sqs_queue.webhook.name
}

output "webhook_dlq_arn" {
  value = aws_sqs_queue.webhook_dlq.arn
}

output "analytics_events_topic_arn" {
  description = "ARN of the analytics-events SNS topic (matches Go SNS_ANALYTICS_TOPIC_ARN)."
  value       = aws_sns_topic.analytics_events.arn
}

output "analytics_events_topic_name" {
  description = "Name of the analytics-events SNS topic (matches Go SNS_ANALYTICS_TOPIC)."
  value       = aws_sns_topic.analytics_events.name
}

output "analytics_tracker_queue_arn" {
  description = "ARN of the analytics-tracker SQS queue."
  value       = aws_sqs_queue.analytics_tracker.arn
}

output "analytics_tracker_queue_name" {
  description = "Name of the analytics-tracker SQS queue."
  value       = aws_sqs_queue.analytics_tracker.name
}

output "analytics_tracker_queue_url" {
  description = "URL of the analytics-tracker SQS queue (matches Go SQS_ANALYTICS_QUEUE_URL)."
  value       = aws_sqs_queue.analytics_tracker.id
}

output "analytics_tracker_dlq_arn" {
  value = aws_sqs_queue.analytics_tracker_dlq.arn
}
