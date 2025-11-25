variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "ecr_repository_name" {
  description = "Name of the ECR repository"
  type        = string
  default     = "media-service-media-ecr-repository"
}

variable "application_environment" {
  description = "Environment the application is running on. It's either LocalStack or AWS"
  type        = string
}
