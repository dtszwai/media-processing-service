resource "aws_cloudwatch_log_group" "cw_log_group" {
  name              = var.log_group_name
  retention_in_days = 7

  tags = merge(var.additional_tags, {
    Name = "media-service-cw-log-group-${var.log_group_name}"
  })
}
