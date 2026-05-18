variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "media_s3_bucket_name" {
  description = "S3 bucket for media files"
  type        = string
}

variable "media_upload_events_queue_arn" {
  description = "ARN of the SQS queue receiving S3 ObjectCreated notifications. The bucket's queue policy must allow the bucket as source (the sns-sqs module wires that)."
  type        = string
}
