variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "media_s3_bucket_name" {
  description = "S3 bucket for media files"
  type        = string
}
