variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-west-2"
}

variable "media_s3_bucket_name" {
  description = "S3 bucket for media files"
  type        = string
  default     = "media-bucket"
}

variable "media_dynamo_table_name" {
  description = "DynamoDB table for media metadata"
  type        = string
  default     = "media"
}
