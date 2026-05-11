output "media_management_topic_arn" {
  value = aws_sns_topic.media_management_topic.arn
}

output "media_management_sqs_queue_arn" {
  value = aws_sqs_queue.media_management_sqs_queue.arn
}

output "media_management_sqs_queue_url" {
  value = aws_sqs_queue.media_management_sqs_queue.url
}

output "dlq_queue_arn" {
  description = "ARN of the dead-letter queue"
  value       = aws_sqs_queue.media_management_sqs_dlq.arn
}

output "dlq_queue_url" {
  description = "URL of the dead-letter queue"
  value       = aws_sqs_queue.media_management_sqs_dlq.url
}

output "dlq_queue_name" {
  description = "Name of the dead-letter queue"
  value       = aws_sqs_queue.media_management_sqs_dlq.name
}

output "dlq_alarm_arn" {
  description = "ARN of the DLQ CloudWatch alarm (if enabled)"
  value       = var.dlq_alarm_enabled ? aws_cloudwatch_metric_alarm.dlq_not_empty[0].arn : null
}

output "generation_topic_arn" {
  value = aws_sns_topic.generation_jobs_topic.arn
}

output "generation_sqs_queue_arn" {
  value = aws_sqs_queue.generation_jobs_queue.arn
}

output "generation_sqs_queue_url" {
  value = aws_sqs_queue.generation_jobs_queue.url
}

output "generation_dlq_queue_arn" {
  value = aws_sqs_queue.generation_jobs_dlq.arn
}

output "generation_dlq_queue_url" {
  value = aws_sqs_queue.generation_jobs_dlq.url
}

output "generation_paid_sqs_queue_arn" {
  value = aws_sqs_queue.generation_paid_jobs_queue.arn
}

output "generation_paid_sqs_queue_url" {
  value = aws_sqs_queue.generation_paid_jobs_queue.url
}
