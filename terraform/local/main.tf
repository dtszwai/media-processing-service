# LocalStack Terraform Configuration
# Use with: tflocal init && tflocal apply
#
# This is a simplified config for local development.
# It excludes AWS-only resources (VPC, ECS, ECR, ALB) that aren't needed locally.

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
  region                      = var.aws_region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    s3       = "http://localhost:4566"
    dynamodb = "http://localhost:4566"
    sns      = "http://localhost:4566"
    sqs      = "http://localhost:4566"
    lambda   = "http://localhost:4566"
    iam      = "http://localhost:4566"
  }
}

locals {
  lambda_jar_path = "${path.module}/../../app/lambdas/target/media-service-lambdas-1.0.0.jar"
}

# =============================================================================
# S3
# =============================================================================

resource "aws_s3_bucket" "media_bucket" {
  bucket        = var.media_s3_bucket_name
  force_destroy = true
}

# =============================================================================
# DynamoDB
# =============================================================================

resource "aws_dynamodb_table" "media_table" {
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
# SNS
# =============================================================================

resource "aws_sns_topic" "media_management_topic" {
  name = "media-management-topic"
}

# =============================================================================
# SQS
# =============================================================================

resource "aws_sqs_queue" "media_management_dlq" {
  name = "media-management-sqs-dlq"
}

resource "aws_sqs_queue" "media_management_queue" {
  name                       = "media-management-sqs-queue"
  delay_seconds              = 10
  visibility_timeout_seconds = 180 # 6x Lambda timeout
  message_retention_seconds  = 86400

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.media_management_dlq.arn
    maxReceiveCount     = 5
  })
}

# SNS -> SQS subscription
data "aws_iam_policy_document" "sqs_policy" {
  statement {
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.media_management_queue.arn]

    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.media_management_topic.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "media_queue_policy" {
  queue_url = aws_sqs_queue.media_management_queue.id
  policy    = data.aws_iam_policy_document.sqs_policy.json
}

resource "aws_sns_topic_subscription" "sqs_subscription" {
  topic_arn = aws_sns_topic.media_management_topic.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.media_management_queue.arn
}

# =============================================================================
# Lambda
# =============================================================================

data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda_role" {
  name               = "media-service-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_lambda_function" "manage_media" {
  filename         = local.lambda_jar_path
  function_name    = "media-service-manage-media-handler"
  role             = aws_iam_role.lambda_role.arn
  handler          = "com.mediaservice.lambda.ManageMediaHandler::handleRequest"
  source_code_hash = fileexists(local.lambda_jar_path) ? filebase64sha256(local.lambda_jar_path) : null
  runtime          = "java21"
  timeout          = 30
  memory_size      = 1024

  environment {
    variables = {
      AWS_REGION                = var.aws_region
      MEDIA_BUCKET_NAME         = var.media_s3_bucket_name
      MEDIA_DYNAMODB_TABLE_NAME = var.media_dynamo_table_name
      # LocalStack endpoints - Lambda runs on same Docker network
      AWS_S3_ENDPOINT       = "http://localstack:4566"
      AWS_DYNAMODB_ENDPOINT = "http://localstack:4566"
      # OpenTelemetry - use service name (not container name) for Docker DNS
      OTEL_EXPORTER_OTLP_ENDPOINT = "http://grafana:4318"
      OTEL_EXPORTER_OTLP_PROTOCOL = "http/protobuf"
      OTEL_SERVICE_NAME           = "media-service-lambda"
    }
  }
}

# SQS -> Lambda trigger
resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn = aws_sqs_queue.media_management_queue.arn
  function_name    = aws_lambda_function.manage_media.arn
  batch_size       = 1
}

# =============================================================================
# Outputs
# =============================================================================

output "s3_bucket_name" {
  value = aws_s3_bucket.media_bucket.id
}

output "dynamodb_table_name" {
  value = aws_dynamodb_table.media_table.name
}

output "sns_topic_arn" {
  value = aws_sns_topic.media_management_topic.arn
}

output "sqs_queue_url" {
  value = aws_sqs_queue.media_management_queue.url
}

output "lambda_function_name" {
  value = aws_lambda_function.manage_media.function_name
}
