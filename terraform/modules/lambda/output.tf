output "manage_media_function_name" {
  value = aws_lambda_function.manage_media.function_name
}

output "analytics_rollup_function_name" {
  value = aws_lambda_function.analytics_rollup.function_name
}

output "manage_media_function_arn" {
  value = aws_lambda_function.manage_media.arn
}

output "analytics_rollup_function_arn" {
  value = aws_lambda_function.analytics_rollup.arn
}
