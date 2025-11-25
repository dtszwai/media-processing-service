variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "log_group_name" {
  description = "Name of the CloudWatch Logs log group"
  type        = string
}
