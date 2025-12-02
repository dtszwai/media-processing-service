data "aws_iam_policy_document" "assume_role" {
  statement {
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }

    actions = ["sts:AssumeRole"]
  }
}

data "aws_iam_policy_document" "lambda_iam_policy_document" {
  statement {
    sid    = "EC2Networking"
    effect = "Allow"
    actions = [
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses",
      "ec2:AttachNetworkInterface",
      "ec2:CreateNetworkInterface",
      "ec2:CreateNetworkInterfacePermission",
      "ec2:DeleteNetworkInterface",
      "ec2:DeleteNetworkInterfacePermission",
      "ec2:Describe*",
      "ec2:DetachNetworkInterface",
      "ec2:GetSecurityGroupsForVpc"
    ]
    resources = ["*"]
  }

  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]

    resources = ["arn:aws:logs:*:*:*"]
  }

  statement {
    sid    = "S3"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:ListBucket",
      "s3:DeleteObject"
    ]
    resources = [
      var.media_bucket_arn,
      "${var.media_bucket_arn}/*"
    ]
  }

  statement {
    sid    = "DynamoDB"
    effect = "Allow"
    actions = [
      "dynamodb:BatchGetItem",
      "dynamodb:BatchWriteItem",
      "dynamodb:DeleteItem",
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:Query",
      "dynamodb:Scan",
      "dynamodb:UpdateItem"
    ]
    resources = [var.dynamodb_table_arn]
  }

  statement {
    sid    = "SQS"
    effect = "Allow"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes"
    ]
    resources = [var.media_management_sqs_queue_arn]
  }
}

resource "aws_iam_policy" "lambda_iam_policy" {
  name        = "media-service-lambda-policy"
  path        = "/"
  description = "IAM policy for Media Service lambda functions"
  policy      = data.aws_iam_policy_document.lambda_iam_policy_document.json

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-lambda-policy"
    }
  )
}

#######################
# Build Java Lambda JAR #
#######################
locals {
  java_src_files = fileset(var.lambdas_src_path, "src/**/*.java")
  pom_hash       = filesha256("${var.lambdas_src_path}/pom.xml")
  java_file_hashes = {
    for file in local.java_src_files :
    file => filesha256("${var.lambdas_src_path}/${file}")
  }
  combined_hash_input   = join("", concat(values(local.java_file_hashes), [local.pom_hash]))
  source_directory_hash = sha256(local.combined_hash_input)
  lambda_jar_file       = "${var.lambdas_src_path}/target/media-service-lambdas.jar"
}

resource "null_resource" "build_lambda_jar" {
  # Skip rebuild for LocalStack - JAR built via Makefile
  count = var.is_local ? 0 : 1

  provisioner "local-exec" {
    command     = "mvn clean package -DskipTests -q"
    working_dir = var.lambdas_src_path
  }

  triggers = {
    should_trigger_resource = local.source_directory_hash
  }
}

########################
# Manage Media Lambda #
########################
resource "aws_vpc_security_group_egress_rule" "allow_lambda_outbound_traffic" {
  count             = var.is_local ? 0 : 1
  security_group_id = var.lambda_sg
  description       = "Allow all outbound traffic"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"

  tags = merge(var.additional_tags, {
    Name = "media-service-allow-outbound-traffic-lambda"
  })
}

resource "aws_iam_role" "lambda_iam_role" {
  name               = "media-service-lambda-iam-role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-lambda-iam-role"
    }
  )
}

resource "aws_lambda_function" "manage_media" {
  dynamic "vpc_config" {
    for_each = var.is_local ? [] : [1]
    content {
      security_group_ids = [var.lambda_sg]
      subnet_ids         = var.private_subnet_ids
    }
  }

  filename         = local.lambda_jar_file
  function_name    = "media-service-manage-media-handler"
  role             = aws_iam_role.lambda_iam_role.arn
  handler          = "com.mediaservice.lambda.ManageMediaHandler::handleRequest"
  source_code_hash = filebase64sha256(local.lambda_jar_file)
  runtime          = "java21"
  architectures    = [var.lambda_architecture]
  timeout          = 120
  memory_size      = 10240 # 10GB max for large image processing
  publish          = var.is_local ? false : var.enable_snapstart

  ephemeral_storage {
    size = 2048 # 2GB for large file processing
  }

  dynamic "snap_start" {
    for_each = var.is_local ? [] : (var.enable_snapstart ? [1] : [])
    content {
      apply_on = "PublishedVersions"
    }
  }

  environment {
    variables = merge(
      {
        MEDIA_BUCKET_NAME           = var.media_s3_bucket_name
        MEDIA_DYNAMODB_TABLE_NAME   = var.dynamodb_table_name
        OTEL_EXPORTER_OTLP_ENDPOINT = var.otel_exporter_endpoint
        OTEL_SERVICE_NAME           = "media-service-lambda"
        OTEL_TRACES_EXPORTER        = "otlp"
        OTEL_METRICS_EXPORTER       = "otlp"
        OTEL_LOGS_EXPORTER          = "otlp"
        OTEL_EXPORTER_OTLP_PROTOCOL = "http/protobuf"
      },
      var.is_local ? {
        AWS_S3_ENDPOINT       = var.localstack_endpoint
        AWS_DYNAMODB_ENDPOINT = var.localstack_endpoint
      } : {}
    )
  }

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-manage-media-handler"
    }
  )
}

resource "aws_cloudwatch_log_group" "lambda_log_group" {
  count             = var.is_local ? 0 : 1
  depends_on        = [aws_lambda_function.manage_media]
  name              = "/aws/lambda/${aws_lambda_function.manage_media.function_name}"
  retention_in_days = 7

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-lambda-log-group"
    }
  )
}

resource "aws_iam_role_policy_attachment" "lambda_iam_policy_attachment" {
  role       = aws_iam_role.lambda_iam_role.name
  policy_arn = aws_iam_policy.lambda_iam_policy.arn
}

resource "aws_lambda_event_source_mapping" "sqs_event_source_mapping" {
  event_source_arn = var.media_management_sqs_queue_arn
  function_name    = aws_lambda_function.manage_media.arn

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-sqs-event-source-mapping"
    }
  )
}

# =============================================================================
# Analytics Rollup Lambda (Roll-Up Pattern)
# =============================================================================
#
# This Lambda handles:
# - Daily archive to S3 (every day at 2:00 AM)
# - Monthly aggregation (1st of month at 3:00 AM)
# - Monthly archive to S3 (1st of month at 5:00 AM)
#
# Daily persistence is handled by API write-behind (every 5 min).
# Weekly/yearly data is calculated at read-time from DynamoDB.

resource "aws_lambda_function" "analytics_rollup" {
  dynamic "vpc_config" {
    for_each = var.is_local ? [] : [1]
    content {
      security_group_ids = [var.lambda_sg]
      subnet_ids         = var.private_subnet_ids
    }
  }

  filename         = local.lambda_jar_file
  function_name    = "media-service-analytics-rollup-handler"
  role             = aws_iam_role.lambda_iam_role.arn
  handler          = "com.mediaservice.lambda.AnalyticsRollupHandler::handleRequest"
  source_code_hash = filebase64sha256(local.lambda_jar_file)
  runtime          = "java21"
  architectures    = [var.lambda_architecture]
  timeout          = 60 # Longer timeout for batch operations
  memory_size      = 1024
  publish          = var.is_local ? false : var.enable_snapstart

  dynamic "snap_start" {
    for_each = var.is_local ? [] : (var.enable_snapstart ? [1] : [])
    content {
      apply_on = "PublishedVersions"
    }
  }

  environment {
    variables = merge(
      {
        MEDIA_BUCKET_NAME           = var.media_s3_bucket_name
        MEDIA_DYNAMODB_TABLE_NAME   = var.dynamodb_table_name
        OTEL_EXPORTER_OTLP_ENDPOINT = var.otel_exporter_endpoint
        OTEL_SERVICE_NAME           = "media-service-analytics-lambda"
        OTEL_TRACES_EXPORTER        = "otlp"
        OTEL_METRICS_EXPORTER       = "otlp"
        OTEL_LOGS_EXPORTER          = "otlp"
        OTEL_EXPORTER_OTLP_PROTOCOL = "http/protobuf"
        JAVA_TOOL_OPTIONS           = "-XX:+TieredCompilation -XX:TieredStopAtLevel=1"
      },
      var.is_local ? {
        AWS_S3_ENDPOINT       = var.localstack_endpoint
        AWS_DYNAMODB_ENDPOINT = var.localstack_endpoint
      } : {}
    )
  }

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-analytics-rollup-handler"
    }
  )
}

resource "aws_cloudwatch_log_group" "analytics_lambda_log_group" {
  count             = var.is_local ? 0 : 1
  depends_on        = [aws_lambda_function.analytics_rollup]
  name              = "/aws/lambda/${aws_lambda_function.analytics_rollup.function_name}"
  retention_in_days = 7

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-analytics-lambda-log-group"
    }
  )
}

# Analytics DLQ for failed events
resource "aws_sqs_queue" "analytics_dlq" {
  name                      = "analytics-rollup-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-analytics-dlq"
    }
  )
}

# =============================================================================
# EventBridge Rules for Analytics (Scheduled Triggers)
# =============================================================================

# Daily archive at 2:00 AM UTC every day
# Archives yesterday's daily data to S3 for long-term storage
resource "aws_cloudwatch_event_rule" "analytics_daily_archive" {
  name                = "analytics-daily-archive"
  description         = "Archive daily analytics to S3 at 2:00 AM UTC"
  schedule_expression = "cron(0 2 * * ? *)"

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-analytics-daily-archive"
    }
  )
}

resource "aws_cloudwatch_event_target" "analytics_daily_archive_target" {
  rule      = aws_cloudwatch_event_rule.analytics_daily_archive.name
  target_id = "analytics-daily-archive-lambda"
  arn       = aws_lambda_function.analytics_rollup.arn
  input     = jsonencode({ type = "analytics.v1.archive.daily" })
}

resource "aws_lambda_permission" "allow_eventbridge_daily_archive" {
  statement_id  = "AllowEventBridgeDailyArchive"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.analytics_rollup.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.analytics_daily_archive.arn
}

# Monthly rollup at 3:00 AM UTC on 1st of month
# Aggregates daily snapshots into a monthly summary for faster queries
resource "aws_cloudwatch_event_rule" "analytics_monthly_rollup" {
  name                = "analytics-monthly-rollup"
  description         = "Aggregate monthly analytics on 1st at 3:00 AM UTC"
  schedule_expression = "cron(0 3 1 * ? *)"

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-analytics-monthly-rollup"
    }
  )
}

resource "aws_cloudwatch_event_target" "analytics_monthly_rollup_target" {
  rule      = aws_cloudwatch_event_rule.analytics_monthly_rollup.name
  target_id = "analytics-monthly-rollup-lambda"
  arn       = aws_lambda_function.analytics_rollup.arn
  input     = jsonencode({ type = "analytics.v1.rollup.monthly" })
}

resource "aws_lambda_permission" "allow_eventbridge_monthly" {
  statement_id  = "AllowEventBridgeMonthly"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.analytics_rollup.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.analytics_monthly_rollup.arn
}

# Monthly archive at 5:00 AM UTC on 1st of month
# Archives monthly data to S3 for long-term storage
resource "aws_cloudwatch_event_rule" "analytics_monthly_archive" {
  name                = "analytics-monthly-archive"
  description         = "Archive monthly analytics to S3 on 1st at 5:00 AM UTC"
  schedule_expression = "cron(0 5 1 * ? *)"

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-analytics-monthly-archive"
    }
  )
}

resource "aws_cloudwatch_event_target" "analytics_monthly_archive_target" {
  rule      = aws_cloudwatch_event_rule.analytics_monthly_archive.name
  target_id = "analytics-monthly-archive-lambda"
  arn       = aws_lambda_function.analytics_rollup.arn
  input     = jsonencode({ type = "analytics.v1.archive.monthly" })
}

resource "aws_lambda_permission" "allow_eventbridge_archive" {
  statement_id  = "AllowEventBridgeArchive"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.analytics_rollup.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.analytics_monthly_archive.arn
}
