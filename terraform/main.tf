terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.84"
    }
  }

  backend "s3" {
    region         = "us-west-2"
    bucket         = "media-service-terraform-state-bucket"
    key            = "media-service/terraform.tfstate"
    dynamodb_table = "media-service-terraform-state-lock-table"
    encrypt        = true
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
# Networking
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
  source               = "./modules/s3"
  additional_tags      = local.common_tags
  media_s3_bucket_name = var.media_s3_bucket_name
}

module "dynamodb" {
  depends_on = [module.networking]

  source                  = "./modules/dynamodb"
  additional_tags         = local.common_tags
  vpc_id                  = module.networking.vpc_id
  dynamodb_table_name     = var.media_dynamo_table_name
  private_route_table_ids = module.networking.private_route_table_ids
}

# =============================================================================
# Messaging
# =============================================================================

module "sns-sqs" {
  source          = "./modules/sns-sqs"
  additional_tags = local.common_tags
}

# =============================================================================
# Container Registry & Images
# =============================================================================

module "ecr" {
  source                  = "./modules/ecr"
  additional_tags         = local.common_tags
  application_environment = var.application_environment
}

module "api_image" {
  source               = "./modules/docker"
  docker_build_context = "../app/api"
  image_tag_prefix     = "media-service"
  ecr_repository_url   = module.ecr.repository_url
}

# =============================================================================
# Logging
# =============================================================================

module "api_logs" {
  source          = "./modules/cloudwatch"
  additional_tags = local.common_tags
  log_group_name  = "media-service-api"
}

# =============================================================================
# Security Groups
# =============================================================================

module "api_alb_sg" {
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking.vpc_id
  name_prefix     = "api-alb-sg"
  description     = "API load balancer security group"
}

module "api_container_sg" {
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking.vpc_id
  name_prefix     = "api-container-sg"
  description     = "API container security group"
}

module "process_lambda_sg" {
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking.vpc_id
  name_prefix     = "process-lambda-sg"
  description     = "Process media Lambda security group"
}

module "manage_lambda_sg" {
  source          = "./modules/security-group"
  additional_tags = local.common_tags
  vpc_id          = module.networking.vpc_id
  name_prefix     = "manage-lambda-sg"
  description     = "Manage media Lambda security group"
}

# =============================================================================
# ECS (API Service)
# =============================================================================

module "ecs" {
  depends_on = [
    module.dynamodb,
    module.ecr,
    module.s3,
    module.networking
  ]

  source = "./modules/ecs"

  additional_tags = local.common_tags

  vpc_id             = module.networking.vpc_id
  desired_task_count = var.desired_task_count

  app_port            = var.api_port
  dynamodb_table_arn  = module.dynamodb.dynamodb_table_arn
  dynamodb_table_name = module.dynamodb.dynamodb_table_name

  ecr_repository_arn = module.ecr.ecr_repository_arn
  api_image_uri      = module.api_image.image_uri

  media_bucket_arn           = module.s3.media_bucket_arn
  media_management_topic_arn = module.sns-sqs.media_management_topic_arn
  media_s3_bucket_name       = var.media_s3_bucket_name

  application_environment = var.application_environment
  otel_exporter_endpoint  = var.otel_exporter_endpoint

  alb_sg_id          = module.api_alb_sg.id
  container_sg_id    = module.api_container_sg.id
  private_subnet_ids = module.networking.private_subnet_ids
  public_subnet_ids  = module.networking.public_subnet_ids

  app_log_group_name = module.api_logs.log_group_name
}

# =============================================================================
# Lambda Functions
# =============================================================================

module "lambda" {
  depends_on = [
    module.dynamodb,
    module.s3,
    module.networking
  ]

  source = "./modules/lambda"

  additional_tags = local.common_tags

  lambdas_src_path = "../app/lambdas"

  dynamodb_table_arn  = module.dynamodb.dynamodb_table_arn
  dynamodb_table_name = module.dynamodb.dynamodb_table_name

  media_bucket_arn               = module.s3.media_bucket_arn
  media_bucket_id                = module.s3.media_bucket_id
  media_management_sqs_queue_arn = module.sns-sqs.media_management_sqs_queue_arn
  media_s3_bucket_name           = var.media_s3_bucket_name

  otel_exporter_endpoint = var.otel_exporter_endpoint

  process_lambda_sg = module.process_lambda_sg.id
  manage_lambda_sg  = module.manage_lambda_sg.id

  private_subnet_ids = module.networking.private_subnet_ids
}
