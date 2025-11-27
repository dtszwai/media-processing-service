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
  lambda_jar_file       = "${var.lambdas_src_path}/target/media-service-lambdas-1.0.0.jar"
}

resource "null_resource" "build_lambda_jar" {
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
  depends_on = [null_resource.build_lambda_jar]

  vpc_config {
    security_group_ids = [var.lambda_sg]
    subnet_ids         = var.private_subnet_ids
  }

  filename         = local.lambda_jar_file
  function_name    = "media-service-manage-media-handler"
  role             = aws_iam_role.lambda_iam_role.arn
  handler          = "com.mediaservice.lambda.ManageMediaHandler::handleRequest"
  source_code_hash = filebase64sha256(local.lambda_jar_file)
  runtime          = "java21"
  architectures    = [var.lambda_architecture]
  timeout          = 30
  memory_size      = 1024
  publish          = var.enable_snapstart

  dynamic "snap_start" {
    for_each = var.enable_snapstart ? [1] : []
    content {
      apply_on = "PublishedVersions"
    }
  }

  environment {
    variables = {
      MEDIA_BUCKET_NAME           = var.media_s3_bucket_name
      MEDIA_DYNAMODB_TABLE_NAME   = var.dynamodb_table_name
      OTEL_EXPORTER_OTLP_ENDPOINT = var.otel_exporter_endpoint
      OTEL_SERVICE_NAME           = "media-service-lambda"
      JAVA_TOOL_OPTIONS           = "-XX:+TieredCompilation -XX:TieredStopAtLevel=1"
    }
  }

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-manage-media-handler"
    }
  )
}

resource "aws_cloudwatch_log_group" "lambda_log_group" {
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
