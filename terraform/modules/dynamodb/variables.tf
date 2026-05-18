variable "additional_tags" {
  description = "Additional tags to apply to resources."
  type        = map(string)
  default     = {}
}

variable "vpc_id" {
  description = "VPC id for the DynamoDB VPC endpoint."
  type        = string
}

variable "private_route_table_ids" {
  description = "Private route tables to associate with the VPC endpoint."
  type        = list(string)
}

variable "dynamodb_table_name" {
  description = "Name of the DynamoDB single-table v2 table. Must match the Go DDB_TABLE env var."
  type        = string
}
