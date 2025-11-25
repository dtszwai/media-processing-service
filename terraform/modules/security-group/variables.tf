variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "name_prefix" {
  description = "Prefix to apply to resource names"
  type        = string
}

variable "description" {
  description = "Description of the security group"
  type        = string
}
