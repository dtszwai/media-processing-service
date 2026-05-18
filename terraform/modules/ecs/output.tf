output "alb_dns_name" {
  description = "Public DNS name of the api ALB."
  value       = aws_alb.api.dns_name
}

output "alb_arn" {
  value = aws_alb.api.arn
}

output "cluster_name" {
  value = aws_ecs_cluster.main.name
}

output "service_name" {
  value = aws_ecs_service.api.name
}
