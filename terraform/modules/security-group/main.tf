resource "aws_security_group" "security_group" {
  description = var.description
  name_prefix = var.name_prefix
  vpc_id      = var.vpc_id

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-${var.name_prefix}-security-group"
    }
  )
}
