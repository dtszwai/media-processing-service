variable "additional_tags" {
  description = "Additional tags to apply to resources"
  type        = map(string)
  default     = {}
}

variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "private_route_table_ids" {
  description = "IDs of the private route tables"
  type        = list(string)
}

variable "dynamodb_table_name" {
  description = "Name of the DynamoDB table"
  type        = string
}
