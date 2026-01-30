# terraform/modules/cloudfront/variables.tf
variable "additional_tags" {
  type    = map(string)
  default = {}
}

variable "s3_bucket_id" {
  description = "S3 bucket ID for origin"
  type        = string
}

variable "s3_bucket_arn" {
  description = "S3 bucket ARN for policy"
  type        = string
}

variable "s3_bucket_regional_domain_name" {
  description = "S3 bucket regional domain name"
  type        = string
}

variable "price_class" {
  description = "CloudFront price class"
  type        = string
  default     = "PriceClass_100" # North America + Europe
}
