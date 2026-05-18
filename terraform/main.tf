# Single apply target: LocalStack Community via `tflocal apply`.
#
# Unsupported AWS-shape references stay gated with `count = 0` only where the
# missing LocalStack Community surface is the reason the module cannot run
# locally.

terraform {
  required_version = ">= 1.7.0"

  required_providers {
    aws = { source = "hashicorp/aws", version = ">= 5.0, < 6.0" }
  }
}

provider "aws" {
  region = var.aws_region

  # LocalStack credentials are literal "test"/"test"; SDK overrides skip the
  # checks that would otherwise reach real AWS.
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  access_key                  = "test"
  secret_key                  = "test"

  endpoints {
    s3                   = local.localstack_provider_endpoint
    dynamodb             = local.localstack_provider_endpoint
    sns                  = local.localstack_provider_endpoint
    sqs                  = local.localstack_provider_endpoint
    sts                  = local.localstack_provider_endpoint
    lambda               = local.localstack_provider_endpoint
    iam                  = local.localstack_provider_endpoint
    cloudwatchlogs       = local.localstack_provider_endpoint
    events               = local.localstack_provider_endpoint
    scheduler            = local.localstack_provider_endpoint
    secretsmanager       = local.localstack_provider_endpoint
    ecs                  = local.localstack_provider_endpoint
    elasticloadbalancing = local.localstack_provider_endpoint
    cloudfront           = local.localstack_provider_endpoint
  }
}

locals {
  localstack_provider_endpoint = var.localstack_provider_endpoint
  localstack_runtime_endpoint  = var.localstack_runtime_endpoint

  common_tags = merge(var.tags, {
    Environment = "localstack"
  })

  vpc_cidr             = "10.0.0.0/24"
  public_subnet_cidrs  = ["10.0.0.0/26", "10.0.0.64/26"]
  private_subnet_cidrs = ["10.0.0.128/26", "10.0.0.192/26"]
}

# =============================================================================
# Networking — VPC, subnets, NAT, route tables. LocalStack mocks these at the
# API surface (no real network isolation), but creating them keeps the
# topology shape complete and lets dependents like the DynamoDB VPC endpoint
# reference real subnet/route-table IDs.
# =============================================================================

module "networking" {
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
  source                        = "./modules/s3"
  additional_tags               = local.common_tags
  media_s3_bucket_name          = var.media_s3_bucket_name
  media_upload_events_queue_arn = module.sns_sqs.media_upload_events_queue_arn

  depends_on = [module.sns_sqs]
}

module "dynamodb" {
  source                  = "./modules/dynamodb"
  additional_tags         = local.common_tags
  vpc_id                  = module.networking.vpc_id
  private_route_table_ids = module.networking.private_route_table_ids
  dynamodb_table_name     = var.media_dynamodb_table_name
}

# =============================================================================
# Messaging
# =============================================================================

module "sns_sqs" {
  source            = "./modules/sns-sqs"
  name_prefix       = var.name_prefix
  additional_tags   = local.common_tags
  media_bucket_name = var.media_s3_bucket_name
}

# =============================================================================
# KMS — prompt envelope key (AES-256-GCM data-key wrapper for prompts at rest).
# LocalStack Community supports KMS; the real key id flows into Lambda env so
# the bootstrap doesn't need `ensureLocalPromptKey` for the Lambda path.
# =============================================================================

module "kms" {
  source          = "./modules/kms"
  name_prefix     = var.name_prefix
  additional_tags = local.common_tags
}

# =============================================================================
# CDN
#
# count = 0: LocalStack Community does not support CloudFront. The module
# stays in source as a reference for the production fronting of the S3 media
# bucket. Flip count to 1 only when applying against real AWS or LocalStack Pro.
# =============================================================================

module "cloudfront" {
  count  = 0
  source = "./modules/cloudfront"

  additional_tags                = local.common_tags
  s3_bucket_id                   = module.s3.media_bucket_id
  s3_bucket_arn                  = module.s3.media_bucket_arn
  s3_bucket_regional_domain_name = module.s3.media_bucket_regional_domain_name
}

# =============================================================================
# ECS (Fargate cluster, ALB, task def, autoscaling)
#
# count = 0: LocalStack Community does not support ECS. The api runs as a
# docker compose service in deploy/compose/local.yaml; this module stays in
# source as a reference for the prod-shape Fargate topology. Required inputs
# are still wired (count = 0 doesn't skip variable validation), but never
# resolved at apply time.
# =============================================================================

module "ecs" {
  count = 0

  source = "./modules/ecs"

  additional_tags = local.common_tags

  vpc_id   = module.networking.vpc_id
  app_port = var.api_port

  ecr_repository_arn = "arn:aws:ecr:${var.aws_region}:000000000000:repository/media-service-api"
  api_image_uri      = "media-service-api:reference"

  dynamodb_table_name = module.dynamodb.dynamodb_table_name
  dynamodb_table_arn  = module.dynamodb.dynamodb_table_arn

  media_bucket_arn     = module.s3.media_bucket_arn
  media_s3_bucket_name = module.s3.media_bucket_name

  media_topic_arn                = module.sns_sqs.media_topic_arn
  media_topic_name               = module.sns_sqs.media_topic_name
  media_cleanup_topic_arn        = module.sns_sqs.media_cleanup_topic_arn
  media_cleanup_topic_name       = module.sns_sqs.media_cleanup_topic_name
  generation_topic_arn           = module.sns_sqs.generation_topic_arn
  generation_topic_name          = module.sns_sqs.generation_topic_name
  generation_queue_arns          = module.sns_sqs.generation_queue_arns
  generation_queue_urls          = module.sns_sqs.generation_queue_urls
  media_queue_arn                = module.sns_sqs.media_queue_arn
  media_queue_name               = module.sns_sqs.media_queue_name
  media_queue_url                = module.sns_sqs.media_queue_url
  media_cleanup_queue_arn        = module.sns_sqs.media_cleanup_queue_arn
  media_cleanup_queue_name       = module.sns_sqs.media_cleanup_queue_name
  media_cleanup_queue_url        = module.sns_sqs.media_cleanup_queue_url
  media_upload_events_queue_arn  = module.sns_sqs.media_upload_events_queue_arn
  media_upload_events_queue_name = module.sns_sqs.media_upload_events_queue_name
  media_upload_events_queue_url  = module.sns_sqs.media_upload_events_queue_url
  webhook_queue_arn              = module.sns_sqs.webhook_queue_arn
  webhook_queue_name             = module.sns_sqs.webhook_queue_name
  webhook_queue_url              = module.sns_sqs.webhook_queue_url

  analytics_events_topic_arn   = module.sns_sqs.analytics_events_topic_arn
  analytics_events_topic_name  = module.sns_sqs.analytics_events_topic_name
  analytics_tracker_queue_arn  = module.sns_sqs.analytics_tracker_queue_arn
  analytics_tracker_queue_name = module.sns_sqs.analytics_tracker_queue_name
  analytics_tracker_queue_url  = module.sns_sqs.analytics_tracker_queue_url

  kms_prompt_key_id  = module.kms.prompt_key_id
  kms_prompt_key_arn = module.kms.prompt_key_arn

  application_environment = "localstack"
  desired_task_count      = 1
  otel_exporter_endpoint  = var.otel_exporter_endpoint

  alb_sg_id          = "sg-api-alb-reference"
  container_sg_id    = "sg-api-container-reference"
  private_subnet_ids = module.networking.private_subnet_ids
  public_subnet_ids  = module.networking.public_subnet_ids

  app_log_group_name = "/ecs/${var.name_prefix}-api"

  depends_on = [module.dynamodb, module.s3, module.sns_sqs]
}

# =============================================================================
# Lambdas — Go bootstrap zips wired against LocalStack's Lambda + SQS pumps.
#
# The generation-worker SQS event source mappings are intentionally not
# created (see modules/lambda/main.tf for the why). The compose
# generation-worker drains the generation queues by long polling instead.
# =============================================================================

module "lambda" {
  source = "./modules/lambda"

  additional_tags     = local.common_tags
  name_prefix         = var.name_prefix
  localstack_endpoint = local.localstack_runtime_endpoint
  aws_region          = var.aws_region

  media_s3_bucket_name      = var.media_s3_bucket_name
  media_dynamodb_table_name = var.media_dynamodb_table_name

  media_bucket_arn   = module.s3.media_bucket_arn
  dynamodb_table_arn = module.dynamodb.dynamodb_table_arn

  media_topic_arn             = module.sns_sqs.media_topic_arn
  media_topic_name            = module.sns_sqs.media_topic_name
  media_cleanup_topic_arn     = module.sns_sqs.media_cleanup_topic_arn
  media_cleanup_topic_name    = module.sns_sqs.media_cleanup_topic_name
  generation_topic_arn        = module.sns_sqs.generation_topic_arn
  generation_topic_name       = module.sns_sqs.generation_topic_name
  analytics_events_topic_arn  = module.sns_sqs.analytics_events_topic_arn
  analytics_events_topic_name = module.sns_sqs.analytics_events_topic_name

  media_queue_arn                = module.sns_sqs.media_queue_arn
  media_queue_url                = module.sns_sqs.media_queue_url
  media_queue_name               = module.sns_sqs.media_queue_name
  generation_queue_arns          = module.sns_sqs.generation_queue_arns
  generation_dlq_arns            = module.sns_sqs.generation_dlq_arns
  generation_queue_urls          = module.sns_sqs.generation_queue_urls
  webhook_queue_arn              = module.sns_sqs.webhook_queue_arn
  webhook_queue_url              = module.sns_sqs.webhook_queue_url
  webhook_queue_name             = module.sns_sqs.webhook_queue_name
  media_cleanup_queue_arn        = module.sns_sqs.media_cleanup_queue_arn
  media_cleanup_queue_name       = module.sns_sqs.media_cleanup_queue_name
  media_cleanup_queue_url        = module.sns_sqs.media_cleanup_queue_url
  media_upload_events_queue_arn  = module.sns_sqs.media_upload_events_queue_arn
  media_upload_events_queue_name = module.sns_sqs.media_upload_events_queue_name
  media_upload_events_queue_url  = module.sns_sqs.media_upload_events_queue_url
  analytics_tracker_queue_arn    = module.sns_sqs.analytics_tracker_queue_arn
  analytics_tracker_queue_name   = module.sns_sqs.analytics_tracker_queue_name
  analytics_tracker_queue_url    = module.sns_sqs.analytics_tracker_queue_url

  kms_prompt_key_id      = module.kms.prompt_key_id
  kms_prompt_key_arn     = module.kms.prompt_key_arn
  otel_exporter_endpoint = var.otel_exporter_endpoint

  lease_reaper_tenants = var.lease_reaper_tenants

  depends_on = [module.dynamodb, module.s3, module.sns_sqs]
}

# =============================================================================
# Outputs
# =============================================================================

output "s3_bucket_name" {
  value = module.s3.media_bucket_name
}

output "dynamodb_table_name" {
  value = module.dynamodb.dynamodb_table_name
}

output "media_topic_arn" {
  value = module.sns_sqs.media_topic_arn
}

output "generation_topic_arn" {
  value = module.sns_sqs.generation_topic_arn
}

output "media_queue_url" {
  value = module.sns_sqs.media_queue_url
}

output "webhook_queue_url" {
  value = module.sns_sqs.webhook_queue_url
}

output "generation_queue_urls" {
  description = "Per-tier × resource-class generation queue URLs."
  value       = module.sns_sqs.generation_queue_urls
}

output "lambda_function_names" {
  description = "Map of function key → AWS Lambda function name."
  value       = module.lambda.function_names
}
