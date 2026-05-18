variable "name_prefix" {
  description = "Prefix applied to every SNS / SQS resource."
  type        = string
}

variable "additional_tags" {
  description = "Additional tags to apply to all resources."
  type        = map(string)
  default     = {}
}

variable "dlq_alarm_threshold" {
  description = "DLQ depth that trips the ApproximateNumberOfMessagesVisible alarm. 1 enforces that any sustained DLQ backlog is actionable."
  type        = number
  default     = 1
}

variable "media_bucket_name" {
  description = "Name of the media S3 bucket. The module constructs the bucket ARN from this so the queue policy can scope publish access to one bucket without creating a module-cycle with the s3 module."
  type        = string
}
