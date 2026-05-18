variable "name_prefix" {
  description = "Prefix applied to key alias and tags."
  type        = string
}

variable "additional_tags" {
  description = "Additional tags applied to all resources."
  type        = map(string)
  default     = {}
}
