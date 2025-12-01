variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "media_s3_bucket_name" {
  description = "S3 bucket for media files"
  type        = string
}

variable "is_local" {
  description = "Whether running in LocalStack (enables force_destroy)"
  type        = bool
  default     = false
}
