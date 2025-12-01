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

# =============================================================================
# Messaging
# =============================================================================

module "sns-sqs" {
  source          = "./modules/sns-sqs"
  additional_tags = local.common_tags
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

  media_bucket_arn               = module.s3.media_bucket_arn
  media_management_sqs_queue_arn = module.sns-sqs.media_management_sqs_queue_arn
  media_s3_bucket_name           = var.media_s3_bucket_name

  otel_exporter_endpoint = var.otel_exporter_endpoint

  # VPC config - empty for LocalStack
  lambda_sg          = var.is_local ? "" : module.lambda_sg[0].id
  private_subnet_ids = var.is_local ? [] : module.networking[0].private_subnet_ids

  # LocalStack settings
  is_local            = var.is_local
  localstack_endpoint = var.localstack_lambda_endpoint

  # SnapStart disabled for LocalStack
  enable_snapstart = var.is_local ? false : var.enable_snapstart
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
