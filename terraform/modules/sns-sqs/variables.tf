variable "media_mngmt_topic_name" {
  description = "Name of the SNS topic"
  type        = string
  default     = "media-management-topic"
}

variable "media_mngmt_queue_name" {
  description = "Name of the SQS queue"
  type        = string
  default     = "media-management-sqs-queue"
}

variable "media_mngmt_dlq_name" {
  description = "Name of the SQS dead-letter queue"
  type        = string
  default     = "media-management-sqs-dlq"
}

variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "dlq_alarm_enabled" {
  description = "Enable CloudWatch alarm for DLQ"
  type        = bool
  default     = true
}

variable "dlq_alarm_threshold" {
  description = "Number of messages in DLQ to trigger alarm"
  type        = number
  default     = 1
}
