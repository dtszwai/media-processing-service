output "media_management_topic_arn" {
  value = aws_sns_topic.media_management_topic.arn
}

output "media_management_sqs_queue_arn" {
  value = aws_sqs_queue.media_management_sqs_queue.arn
}

output "media_management_sqs_queue_url" {
  value = aws_sqs_queue.media_management_sqs_queue.url
}
