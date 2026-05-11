# Unified Terraform Configuration
# Works with both LocalStack (local dev) and AWS (production)
#
# Usage:
#   LocalStack: tflocal init && tflocal apply -var-file=local.tfvars
#   AWS:        terraform init && terraform apply -var-file=prod.tfvars

terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.84"
    }
  }
}

# ARN of the Secrets Manager entry holding the real OpenAI API key in non-local
# environments. Empty allows is_local=true (LocalStack provisions its own stub
# below). The lambda module enforces this is set when is_local = false via a
# lifecycle.precondition on aws_lambda_function.generation_worker.
variable "generation_openai_api_key_secret_arn" {
  description = "Secrets Manager ARN for the generation OpenAI API key (required when is_local = false)."
  type        = string
  default     = ""
}

provider "aws" {
  region = var.aws_region

  # LocalStack-specific settings
  skip_credentials_validation = var.is_local
  skip_metadata_api_check     = var.is_local
  skip_requesting_account_id  = var.is_local
  access_key                  = var.is_local ? "test" : null
  secret_key                  = var.is_local ? "test" : null

  dynamic "endpoints" {
    for_each = var.is_local ? [1] : []
    content {
      s3             = var.localstack_endpoint
      dynamodb       = var.localstack_endpoint
      sns            = var.localstack_endpoint
      sqs            = var.localstack_endpoint
      lambda         = var.localstack_endpoint
      iam            = var.localstack_endpoint
      cloudwatchlogs = var.localstack_endpoint
      events         = var.localstack_endpoint
      secretsmanager = var.localstack_endpoint
    }
  }
}

locals {
  common_tags = {
    App = "media-service"
  }
  vpc_cidr             = "10.0.0.0/24"
  public_subnet_cidrs  = ["10.0.0.0/26", "10.0.0.64/26"]
  private_subnet_cidrs = ["10.0.0.128/26", "10.0.0.192/26"]
}

# =============================================================================
# Networking (AWS only - not needed for LocalStack)
# =============================================================================

module "networking" {
  count                = var.is_local ? 0 : 1
  source               = "./modules/networking"
  additional_tags      = local.common_tags
  vpc_cidr             = local.vpc_cidr
  public_subnet_cidrs  = local.public_subnet_cidrs
  private_subnet_cidrs = local.private_subnet_cidrs
}

# =============================================================================
# Storage
# =============================================================================

module "s3" {
  source               = "./modules/s3"
  additional_tags      = local.common_tags
  media_s3_bucket_name = var.media_s3_bucket_name
  is_local             = var.is_local
}

module "dynamodb" {
  count      = var.is_local ? 0 : 1
  depends_on = [module.networking]

  source                  = "./modules/dynamodb"
  additional_tags         = local.common_tags
  vpc_id                  = module.networking[0].vpc_id
  dynamodb_table_name     = var.media_dynamo_table_name
  private_route_table_ids = module.networking[0].private_route_table_ids
}

# DynamoDB for LocalStack (simplified, no VPC endpoint)
resource "aws_dynamodb_table" "media_table_local" {
  count        = var.is_local ? 1 : 0
  name         = var.media_dynamo_table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  attribute {
    name = "createdAt"
    type = "S"
  }

  global_secondary_index {
    name            = "SK-createdAt-index"
    hash_key        = "SK"
    range_key       = "createdAt"
    projection_type = "ALL"
  }

  ttl {
    attribute_name = "expiresAt"
    enabled        = true
  }
}

resource "aws_secretsmanager_secret" "generation_openai_api_key_local" {
  count                   = var.is_local ? 1 : 0
  name                    = "media-service/local/generation/openai-api-key"
  recovery_window_in_days = 0

  tags = merge(local.common_tags, {
    Name = "media-service-local-generation-openai-api-key"
  })
}

resource "aws_secretsmanager_secret_version" "generation_openai_api_key_local" {
  count         = var.is_local ? 1 : 0
  secret_id     = aws_secretsmanager_secret.generation_openai_api_key_local[0].id
  secret_string = jsonencode({ api_key = "not-configured" })
}

# =============================================================================
# Messaging
# =============================================================================

module "sns-sqs" {
  source            = "./modules/sns-sqs"
  additional_tags   = local.common_tags
  dlq_alarm_enabled = var.is_local ? false : true
}

# TODO: align metric name with plan (used_usd vs used_pct). Worker currently
# emits `generation.budget.used_pct`; the plan calls the source metric
# `generation.budget.used_usd`. Keeping `used_pct` here to avoid forcing app-code
# churn from this layer.
resource "aws_cloudwatch_metric_alarm" "generation_budget_used_pct" {
  count = var.is_local ? 0 : 1

  alarm_name          = "generation-budget-used-80pct"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "generation.budget.used_pct"
  namespace           = "MediaService/Generation"
  period              = 300
  statistic           = "Maximum"
  threshold           = 80
  alarm_description   = "Daily generation budget at or above 80 percent of cap"
  treat_missing_data  = "notBreaching"

  dimensions = {
    service = "generation-worker"
  }

  tags = merge(local.common_tags, {
    Name = "generation-budget-used-80pct"
  })
}

resource "aws_cloudwatch_metric_alarm" "generation_budget_used_100pct" {
  count = var.is_local ? 0 : 1

  alarm_name          = "generation-budget-used-100pct"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "generation.budget.used_pct"
  namespace           = "MediaService/Generation"
  period              = 300
  statistic           = "Maximum"
  threshold           = 100
  alarm_description   = "Daily generation budget fully exhausted"
  treat_missing_data  = "notBreaching"

  dimensions = {
    service = "generation-worker"
  }

  # Distinct alarm action from the 80% alarm (e.g. page-on-call SNS) is wired
  # separately via alarm_actions when the on-call topic ARN is provided.

  tags = merge(local.common_tags, {
    Name = "generation-budget-used-100pct"
  })
}

resource "aws_cloudwatch_dashboard" "generation_service" {
  count = var.is_local ? 0 : 1

  dashboard_name = "media-service-generation"
  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "Generation queue depth and age"
          metrics = [
            ["AWS/SQS", "ApproximateNumberOfMessagesVisible", "QueueName", "generation-jobs", { stat = "Maximum" }],
            [".", "ApproximateNumberOfMessagesNotVisible", ".", ".", { stat = "Maximum" }],
            [".", "ApproximateAgeOfOldestMessage", ".", ".", { stat = "Maximum", yAxis = "right" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "Generation worker errors, throttles, and duration"
          metrics = [
            ["AWS/Lambda", "Errors", "FunctionName", module.lambda.generation_worker_function_name, { stat = "Sum" }],
            [".", "Throttles", ".", ".", { stat = "Sum" }],
            [".", "Duration", ".", ".", { stat = "p95", yAxis = "right" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "Generation stage latency"
          metrics = [
            ["MediaService/Generation", "generation.stage.latency_ms", "service", "generation-worker", { stat = "p50" }],
            [".", ".", ".", ".", { stat = "p95" }],
            [".", ".", ".", ".", { stat = "p99" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 6
        width  = 12
        height = 6
        properties = {
          region = var.aws_region
          title  = "Generation cost guardrails"
          metrics = [
            ["MediaService/Generation", "BudgetUsedPct", "Service", "media-service-generation", { stat = "Maximum" }],
            [".", "generation.artifact.cost_usd", "service", "generation-worker", { stat = "Sum", yAxis = "right" }]
          ]
        }
      }
    ]
  })
}

# =============================================================================
# CDN (AWS only - CloudFront for preview images)
# =============================================================================

module "cloudfront" {
  count  = var.is_local ? 0 : 1
  source = "./modules/cloudfront"

  additional_tags                = local.common_tags
  s3_bucket_id                   = module.s3.media_bucket_id
  s3_bucket_arn                  = module.s3.media_bucket_arn
  s3_bucket_regional_domain_name = module.s3.media_bucket_regional_domain_name
}

# =============================================================================
# Container Registry & Images (AWS only)
# =============================================================================

module "ecr" {
  count                   = var.is_local ? 0 : 1
  source                  = "./modules/ecr"
  additional_tags         = local.common_tags
  application_environment = var.application_environment
}

module "api_image" {
  count                = var.is_local ? 0 : 1
  source               = "./modules/docker"
  docker_build_context = "../app/api"
  image_tag_prefix     = "media-service"
  ecr_repository_url   = module.ecr[0].repository_url
}

# =============================================================================
# Logging
# =============================================================================

module "api_logs" {
  count           = var.is_local ? 0 : 1
  source          = "./modules/cloudwatch"
  additional_tags = local.common_tags
  log_group_name  = "media-service-api"
}

# =============================================================================
# Security Groups (AWS only)
# =============================================================================

module "api_alb_sg" {
  count           = var.is_local ? 0 : 1
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking[0].vpc_id
  name_prefix     = "api-alb-sg"
  description     = "API load balancer security group"
}

module "api_container_sg" {
  count           = var.is_local ? 0 : 1
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking[0].vpc_id
  name_prefix     = "api-container-sg"
  description     = "API container security group"
}

module "lambda_sg" {
  count           = var.is_local ? 0 : 1
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking[0].vpc_id
  name_prefix     = "lambda-sg"
  description     = "Lambda security group"
}

# =============================================================================
# ECS (API Service - AWS only)
# =============================================================================

module "ecs" {
  count = var.is_local ? 0 : 1
  depends_on = [
    module.dynamodb,
    module.ecr,
    module.s3,
    module.networking
  ]

  source = "./modules/ecs"

  additional_tags = local.common_tags

  vpc_id             = module.networking[0].vpc_id
  desired_task_count = var.desired_task_count

  app_port            = var.api_port
  dynamodb_table_arn  = module.dynamodb[0].dynamodb_table_arn
  dynamodb_table_name = module.dynamodb[0].dynamodb_table_name

  ecr_repository_arn = module.ecr[0].ecr_repository_arn
  api_image_uri      = module.api_image[0].image_uri

  media_bucket_arn           = module.s3.media_bucket_arn
  media_management_topic_arn = module.sns-sqs.media_management_topic_arn
  generation_topic_arn       = module.sns-sqs.generation_topic_arn
  generation_queue_arn       = module.sns-sqs.generation_sqs_queue_arn
  generation_queue_url       = module.sns-sqs.generation_sqs_queue_url
  media_s3_bucket_name       = var.media_s3_bucket_name

  application_environment = var.application_environment
  otel_exporter_endpoint  = var.otel_exporter_endpoint

  alb_sg_id          = module.api_alb_sg[0].id
  container_sg_id    = module.api_container_sg[0].id
  private_subnet_ids = module.networking[0].private_subnet_ids
  public_subnet_ids  = module.networking[0].public_subnet_ids

  app_log_group_name = module.api_logs[0].log_group_name
}

# =============================================================================
# Lambda Functions
# =============================================================================

module "lambda" {
  # Note: depends_on can't be conditional, but these modules are safe
  # because we use count to skip them when is_local=true
  depends_on = [module.s3, module.sns-sqs]

  source = "./modules/lambda"

  additional_tags = local.common_tags

  lambdas_src_path = "../app/lambdas"

  # DynamoDB ARN - use local table or module
  dynamodb_table_arn  = var.is_local ? aws_dynamodb_table.media_table_local[0].arn : module.dynamodb[0].dynamodb_table_arn
  dynamodb_table_name = var.media_dynamo_table_name

  media_bucket_arn                     = module.s3.media_bucket_arn
  media_management_sqs_queue_arn       = module.sns-sqs.media_management_sqs_queue_arn
  generation_sqs_queue_arn             = module.sns-sqs.generation_sqs_queue_arn
  generation_paid_sqs_queue_arn        = module.sns-sqs.generation_paid_sqs_queue_arn
  generation_topic_arn                 = module.sns-sqs.generation_topic_arn
  generation_openai_api_key_secret_arn = var.is_local ? aws_secretsmanager_secret.generation_openai_api_key_local[0].arn : var.generation_openai_api_key_secret_arn
  media_s3_bucket_name                 = var.media_s3_bucket_name

  region = var.aws_region

  otel_exporter_endpoint = var.otel_exporter_endpoint

  # VPC config - empty for LocalStack
  lambda_sg          = var.is_local ? "" : module.lambda_sg[0].id
  private_subnet_ids = var.is_local ? [] : module.networking[0].private_subnet_ids

  # LocalStack settings
  is_local            = var.is_local
  localstack_endpoint = var.localstack_lambda_endpoint

  # SnapStart disabled for LocalStack
  enable_snapstart = var.is_local ? false : var.enable_snapstart

  # Webhook HMAC signing secret
  webhook_secret = var.webhook_secret

  # Container-image deployment for the generation-worker (AWS only — LocalStack
  # community does not support container Lambdas). When set, the image bundles
  # Python + notebooklm-py alongside the JAR for the NotebookLM provider.
  generation_worker_image_uri = var.generation_worker_image_uri

  # When true, skip the generation-worker SQS event source mappings so the API
  # container's in-process stage poller is the sole consumer. LocalStack path.
  local_stage_poller_enabled = var.local_stage_poller_enabled
}

# =============================================================================
# Outputs
# =============================================================================

output "s3_bucket_name" {
  value = module.s3.media_bucket_id
}

output "dynamodb_table_name" {
  value = var.media_dynamo_table_name
}

output "sns_topic_arn" {
  value = module.sns-sqs.media_management_topic_arn
}

output "sqs_queue_url" {
  value = module.sns-sqs.media_management_sqs_queue_url
}

output "lambda_function_name" {
  value = module.lambda.manage_media_function_name
}

output "analytics_lambda_function_name" {
  value = module.lambda.analytics_rollup_function_name
}

output "cloudfront_domain" {
  value = var.is_local ? "" : module.cloudfront[0].distribution_domain_name
}

output "dlq_queue_url" {
  description = "URL of the dead-letter queue"
  value       = module.sns-sqs.dlq_queue_url
}

output "dlq_queue_arn" {
  description = "ARN of the dead-letter queue"
  value       = module.sns-sqs.dlq_queue_arn
}

output "generation_topic_arn" {
  value = module.sns-sqs.generation_topic_arn
}

output "generation_sqs_queue_url" {
  value = module.sns-sqs.generation_sqs_queue_url
}

output "generation_dlq_queue_url" {
  value = module.sns-sqs.generation_dlq_queue_url
}

output "generation_lambda_function_name" {
  value = module.lambda.generation_worker_function_name
}

output "generation_openai_api_key_secret_arn" {
  value = var.is_local ? aws_secretsmanager_secret.generation_openai_api_key_local[0].arn : var.generation_openai_api_key_secret_arn
}
