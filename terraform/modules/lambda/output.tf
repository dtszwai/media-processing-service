output "function_arns" {
  description = "Map of function name → ARN."
  value       = { for k, v in aws_lambda_function.fn : k => v.arn }
}

output "function_names" {
  description = "Map of function name → resolved function_name attribute."
  value       = { for k, v in aws_lambda_function.fn : k => v.function_name }
}

output "generation_worker_function_name" {
  value = aws_lambda_function.fn["generation-worker"].function_name
}

output "media_worker_function_name" {
  value = aws_lambda_function.fn["media-worker"].function_name
}

output "webhook_dispatcher_function_name" {
  value = aws_lambda_function.fn["webhook-worker"].function_name
}

output "analytics_rollup_function_name" {
  value = aws_lambda_function.fn["analytics-rollup"].function_name
}

output "lease_reaper_function_name" {
  value = aws_lambda_function.fn["lease-reaper"].function_name
}

output "cleanup_worker_function_name" {
  value = aws_lambda_function.fn["cleanup-worker"].function_name
}

output "analytics_tracker_function_name" {
  value = aws_lambda_function.fn["analytics-worker"].function_name
}
