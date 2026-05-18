# =============================================================================
# Go Lambdas on provided.al2023, arm64, bootstrap zip.
#
# local.functions below is the source of truth for the artifact set. Each zip
# is built externally by `make tf-up`; terraform reads the bootstrap binary
# from backend/dist/lambda/<name>/bootstrap.
# =============================================================================

terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = ">= 5.0, < 6.0" }
    archive = { source = "hashicorp/archive", version = ">= 2.4" }
  }
}

data "aws_region" "current" {}
data "aws_caller_identity" "current" {}

locals {
  lambda_bin_root = "${path.module}/../../../backend/dist/lambda"

  functions = {
    "generation-worker" = {
      timeout = 300
      memory  = 2048
    }
    "media-worker" = {
      timeout = 120
      memory  = 1024
    }
    "webhook-worker" = {
      timeout = 60
      memory  = 512
    }
    "analytics-rollup" = {
      timeout = 300
      memory  = 1024
    }
    "analytics-worker" = {
      timeout = 30
      memory  = 256
    }
    "lease-reaper" = {
      timeout = 60
      memory  = 256
    }
    "cleanup-worker" = {
      timeout = 60
      memory  = 512
    }
    # S3 ObjectCreated → upload-completion failsafe. See cmd/workers/upload-events.
    "upload-events-worker" = {
      timeout = 60
      memory  = 512
    }
    # Scheduler-driven; one Step() pass per shard per stream, then exits.
    # Decoupled from the API process so stream draining survives API outages.
    "outbox-relay" = {
      timeout = 120
      memory  = 512
    }
  }
}

# =============================================================================
# IAM — shared assume-role policy + per-function least-privilege policies.
# =============================================================================

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "common" {
  statement {
    sid       = "CloudWatchLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["arn:aws:logs:*:*:*"]
  }

  statement {
    sid    = "ENI"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "S3"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:ListBucket",
      "s3:AbortMultipartUpload",
    ]
    resources = [
      var.media_bucket_arn,
      "${var.media_bucket_arn}/*",
    ]
  }

  # DynamoDB on the media table + every GSI. IAM does not grant GSI access
  # via the table ARN alone — enumerate each.
  statement {
    sid    = "DynamoDB"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
      "dynamodb:BatchGetItem",
      "dynamodb:BatchWriteItem",
      "dynamodb:Query",
      "dynamodb:Scan",
      "dynamodb:TransactWriteItems",
      "dynamodb:DescribeTable",
    ]
    resources = [
      var.dynamodb_table_arn,
      "${var.dynamodb_table_arn}/index/gsi_job",
      "${var.dynamodb_table_arn}/index/gsi_tenant_media",
      "${var.dynamodb_table_arn}/index/gsi_lease_expiry",
      "${var.dynamodb_table_arn}/index/gsi_lifecycle",
      "${var.dynamodb_table_arn}/index/gsi_audit_entity",
      "${var.dynamodb_table_arn}/index/gsi_audit_actor",
      "${var.dynamodb_table_arn}/index/gsi_asset_role",
    ]
  }

  statement {
    sid       = "SNSPublish"
    effect    = "Allow"
    actions   = ["sns:Publish"]
    resources = [var.media_topic_arn, var.media_cleanup_topic_arn, var.generation_topic_arn, var.analytics_events_topic_arn]
  }

  statement {
    sid       = "SQSSendWebhook"
    effect    = "Allow"
    actions   = ["sqs:SendMessage", "sqs:GetQueueAttributes"]
    resources = [var.webhook_queue_arn]
  }

  # KMS data-key wrapping for prompt envelope encryption (AES-256-GCM, see
  # internal/infra/sealer/impl/kms).
  statement {
    sid    = "KMSPromptEnvelope"
    effect = "Allow"
    actions = [
      "kms:GenerateDataKey",
      "kms:Decrypt",
    ]
    resources = [var.kms_prompt_key_arn]
  }
}

data "aws_iam_policy_document" "sqs_consume" {
  for_each = local.functions

  # SQS receive/delete on the queue(s) this function consumes. The set
  # depends on the function — generation-worker reads from the generation queues,
  # media-worker from media-jobs, webhook-worker from webhook-delivery.
  dynamic "statement" {
    for_each = length(local.function_source_arns[each.key]) > 0 ? [1] : []
    content {
      sid       = "SQSConsume"
      effect    = "Allow"
      actions   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ChangeMessageVisibility"]
      resources = local.function_source_arns[each.key]
    }
  }
}

locals {
  function_source_arns = {
    "generation-worker"    = concat(values(var.generation_queue_arns), values(var.generation_dlq_arns))
    "media-worker"         = [var.media_queue_arn]
    "webhook-worker"       = [var.webhook_queue_arn]
    "analytics-rollup"     = []
    "analytics-worker"     = [var.analytics_tracker_queue_arn]
    "lease-reaper"         = []
    "cleanup-worker"       = [var.media_cleanup_queue_arn]
    "upload-events-worker" = [var.media_upload_events_queue_arn]
    "outbox-relay"         = []
  }
}

resource "aws_iam_role" "function" {
  for_each = local.functions

  name               = "${var.name_prefix}-${each.key}"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-${each.key}"
  })
}

resource "aws_iam_role_policy" "common" {
  for_each = local.functions

  name   = "common"
  role   = aws_iam_role.function[each.key].id
  policy = data.aws_iam_policy_document.common.json
}

resource "aws_iam_role_policy" "sqs_consume" {
  for_each = { for k, v in local.functions : k => v if length(local.function_source_arns[k]) > 0 }

  name   = "sqs-consume"
  role   = aws_iam_role.function[each.key].id
  policy = data.aws_iam_policy_document.sqs_consume[each.key].json
}

# =============================================================================
# Bootstrap zips. The binaries are produced by `make tf-up` before
# every `terraform apply`. archive_file is a data source so it resolves at
# plan time; if the binary is missing, plan fails — that's intentional.
# =============================================================================

data "archive_file" "bootstrap" {
  for_each = local.functions

  type        = "zip"
  source_file = "${local.lambda_bin_root}/${each.key}/bootstrap"
  output_path = "${local.lambda_bin_root}/${each.key}/bootstrap.zip"
}

# =============================================================================
# Lambda functions.
# =============================================================================

resource "aws_lambda_function" "fn" {
  for_each = local.functions

  function_name    = "${var.name_prefix}-${each.key}"
  role             = aws_iam_role.function[each.key].arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.bootstrap[each.key].output_path
  source_code_hash = data.archive_file.bootstrap[each.key].output_base64sha256
  timeout          = each.value.timeout
  memory_size      = each.value.memory

  environment {
    variables = {
      AWS_REGION                        = var.aws_region
      AWS_ENDPOINT_URL                  = var.localstack_endpoint
      S3_BUCKET                         = var.media_s3_bucket_name
      DDB_TABLE                         = var.media_dynamodb_table_name
      SNS_MEDIA_TOPIC                   = var.media_topic_name
      SNS_MEDIA_TOPIC_ARN               = var.media_topic_arn
      SNS_MEDIA_CLEANUP_TOPIC           = var.media_cleanup_topic_name
      SNS_MEDIA_CLEANUP_TOPIC_ARN       = var.media_cleanup_topic_arn
      SNS_GENERATION_TOPIC              = var.generation_topic_name
      SNS_GENERATION_TOPIC_ARN          = var.generation_topic_arn
      SNS_ANALYTICS_TOPIC               = var.analytics_events_topic_name
      SNS_ANALYTICS_TOPIC_ARN           = var.analytics_events_topic_arn
      SQS_MEDIA_QUEUE                   = var.media_queue_name
      SQS_MEDIA_QUEUE_URL               = var.media_queue_url
      SQS_WEBHOOK_QUEUE                 = var.webhook_queue_name
      SQS_WEBHOOK_QUEUE_URL             = var.webhook_queue_url
      SQS_MEDIA_CLEANUP_QUEUE           = var.media_cleanup_queue_name
      SQS_MEDIA_CLEANUP_QUEUE_URL       = var.media_cleanup_queue_url
      SQS_MEDIA_UPLOAD_EVENTS_QUEUE     = var.media_upload_events_queue_name
      SQS_MEDIA_UPLOAD_EVENTS_QUEUE_URL = var.media_upload_events_queue_url
      SQS_GENERATION_QUEUE_URLS         = jsonencode({ for key, url in var.generation_queue_urls : "generation-jobs-${key}" => url })
      SQS_ANALYTICS_QUEUE               = var.analytics_tracker_queue_name
      SQS_ANALYTICS_QUEUE_URL           = var.analytics_tracker_queue_url
      KMS_PROMPT_KEY_ID                 = var.kms_prompt_key_id
      OTEL_EXPORTER_OTLP_ENDPOINT       = var.otel_exporter_endpoint
      OTEL_SERVICE_NAME                 = "${var.name_prefix}-${each.key}"
      WEBHOOK_SECRET                    = var.webhook_secret
      # Selects the embedded YAML overlay shipped inside the binary.
      MSG_ENV              = "localstack"
      LEASE_REAPER_TENANTS = var.lease_reaper_tenants
    }
  }

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-${each.key}"
  })

  depends_on = [aws_cloudwatch_log_group.fn]
}

resource "aws_cloudwatch_log_group" "fn" {
  for_each = local.functions

  name              = "/aws/lambda/${var.name_prefix}-${each.key}"
  retention_in_days = 7

  tags = merge(var.additional_tags, {
    Name = "/aws/lambda/${var.name_prefix}-${each.key}"
  })
}

# =============================================================================
# Event source mappings: SQS → Lambda.
# =============================================================================

# for_each = {}: LocalStack Community's SQS event-source pump lags badly for
# the generation queues, which starves the FSM. The compose
# generation-worker drains them directly by long polling; cmd/api has an
# opt-in fallback for solo-api local setups. To exercise the AWS-shape ESM path
# against real AWS or LocalStack Pro, replace `{}` with
# `var.generation_queue_arns`.
resource "aws_lambda_event_source_mapping" "generation" {
  for_each = {}

  event_source_arn                   = each.value
  function_name                      = aws_lambda_function.fn["generation-worker"].arn
  batch_size                         = 1
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_batching_window_in_seconds = 0
}

resource "aws_lambda_event_source_mapping" "media" {
  event_source_arn        = var.media_queue_arn
  function_name           = aws_lambda_function.fn["media-worker"].arn
  batch_size              = 5
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "webhook" {
  event_source_arn        = var.webhook_queue_arn
  function_name           = aws_lambda_function.fn["webhook-worker"].arn
  batch_size              = 5
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "cleanup" {
  event_source_arn        = var.media_cleanup_queue_arn
  function_name           = aws_lambda_function.fn["cleanup-worker"].arn
  batch_size              = 5
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "upload_events" {
  event_source_arn        = var.media_upload_events_queue_arn
  function_name           = aws_lambda_function.fn["upload-events-worker"].arn
  batch_size              = 5
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "analytics_tracker" {
  event_source_arn        = var.analytics_tracker_queue_arn
  function_name           = aws_lambda_function.fn["analytics-worker"].arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

# =============================================================================
# Cron triggers via EventBridge Scheduler. LocalStack Community supports
# Scheduler, so the rules are live — analytics-rollup is harmless out of UTC
# business hours, and lease-reaper does nothing while LEASE_REAPER_TENANTS is
# empty (the default).
# =============================================================================

resource "aws_iam_role" "scheduler" {
  name = "${var.name_prefix}-scheduler-invoker"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "scheduler.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-scheduler-invoker"
  })
}

resource "aws_iam_role_policy" "scheduler_invoke" {
  name = "invoke-lambda"
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = "lambda:InvokeFunction"
      Resource = [
        aws_lambda_function.fn["analytics-rollup"].arn,
        aws_lambda_function.fn["lease-reaper"].arn,
        aws_lambda_function.fn["outbox-relay"].arn,
      ]
    }]
  })
}

resource "aws_scheduler_schedule" "analytics_daily" {
  name                = "${var.name_prefix}-analytics-daily"
  schedule_expression = "cron(0 2 * * ? *)"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.fn["analytics-rollup"].arn
    role_arn = aws_iam_role.scheduler.arn
    input    = jsonencode({ type = "analytics.v1.rollup.daily" })
  }
}

resource "aws_scheduler_schedule" "lease_reaper" {
  name                = "${var.name_prefix}-lease-reaper"
  schedule_expression = "rate(5 minutes)"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.fn["lease-reaper"].arn
    role_arn = aws_iam_role.scheduler.arn
  }
}

# Outbox relay fires every minute so pending rows publish within a bounded
# horizon. The relay itself is shard-leased; running it more frequently than
# the lease churn just sees no-ops on quiet shards.
resource "aws_scheduler_schedule" "outbox_relay" {
  name                = "${var.name_prefix}-outbox-relay"
  schedule_expression = "rate(1 minute)"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.fn["outbox-relay"].arn
    role_arn = aws_iam_role.scheduler.arn
  }
}

