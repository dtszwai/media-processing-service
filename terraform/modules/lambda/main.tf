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
# Build lambda bundle #
#######################
locals {
  files_to_hash = setsubtract(
    fileset(var.lambdas_src_path, "**/*"),
    fileset(var.lambdas_src_path, "node_modules/**/*")
  )
  file_hashes = {
    for file in local.files_to_hash :
    file => filesha256("${var.lambdas_src_path}/${file}")
  }
  combined_hash_input   = join("", values(local.file_hashes))
  source_directory_hash = sha256(local.combined_hash_input)
  lambda_zip_file       = "${var.lambdas_src_path}/lambda-functions-payload.zip"
}

resource "null_resource" "build_lambda_bundle" {
  provisioner "local-exec" {
    command     = "npm run build"
    working_dir = var.lambdas_src_path
  }

  triggers = {
    should_trigger_resource = local.source_directory_hash
  }
}

data "archive_file" "lambda" {
  type        = "zip"
  source_dir  = "${var.lambdas_src_path}/dist/"
  output_path = local.lambda_zip_file

  depends_on = [null_resource.build_lambda_bundle]
}

#######################################
# Build lambda layer for sharp module #
#######################################
locals {
  sharp_layer_dir_path = "${var.lambdas_src_path}/sharp-layer"
  sharp_layer_zip_file = "${local.sharp_layer_dir_path}/lambda-sharp-layer.zip"
}

resource "null_resource" "build_sharp_lambda_layer" {
  provisioner "local-exec" {
    command     = "sh build-lambda-layer.sh"
    working_dir = local.sharp_layer_dir_path
  }

  triggers = {
    should_trigger_resource = local.source_directory_hash
  }
}

resource "aws_lambda_layer_version" "sharp_lambda_layer" {
  depends_on          = [null_resource.build_sharp_lambda_layer]
  filename            = local.sharp_layer_zip_file
  layer_name          = "media-service-sharp-lambda-layer"
  compatible_runtimes = ["nodejs22.x"]
  source_code_hash    = local.source_directory_hash
}

#######################################
# Build lambda layer for otel module #
#######################################
locals {
  otel_layer_dir_path = "${var.lambdas_src_path}/otel-layer"
  otel_layer_zip_file = "${local.otel_layer_dir_path}/lambda-otel-layer.zip"
}

resource "null_resource" "build_otel_lambda_layer" {
  provisioner "local-exec" {
    command     = "sh build-lambda-layer.sh"
    working_dir = local.otel_layer_dir_path
  }

  triggers = {
    should_trigger_resource = local.source_directory_hash
  }
}

resource "aws_lambda_layer_version" "otel_lambda_layer" {
  depends_on          = [null_resource.build_otel_lambda_layer]
  filename            = local.otel_layer_zip_file
  layer_name          = "media-service-otel-lambda-layer"
  compatible_runtimes = ["nodejs22.x"]
  source_code_hash    = local.source_directory_hash
}

########################
# Manage Media Lambda #
########################
resource "aws_vpc_security_group_egress_rule" "allow_manage_media_lambda_outbound_traffic" {
  security_group_id = var.manage_media_lambda_sg
  description       = "Allow all outbound traffic"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"

  tags = merge(var.additional_tags, {
    Name = "media-service-coll-allow-outbound-traffic-manage-media-lambda"
  })
}

resource "aws_iam_role" "manage_media_iam_role" {
  name               = "media-service-manage-media-iam-role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-manage-media-iam-role"
    }
  )
}

resource "aws_lambda_function" "manage_media" {
  depends_on = [
    aws_lambda_layer_version.sharp_lambda_layer,
    aws_lambda_layer_version.otel_lambda_layer,
  ]
  layers = [
    aws_lambda_layer_version.sharp_lambda_layer.arn,
    aws_lambda_layer_version.otel_lambda_layer.arn
  ]

  vpc_config {
    security_group_ids = [var.manage_media_lambda_sg]
    subnet_ids         = var.private_subnet_ids
  }

  filename         = local.lambda_zip_file
  function_name    = "media-service-manage-media-handler"
  role             = aws_iam_role.manage_media_iam_role.arn
  handler          = "index.handlers.manageMedia"
  source_code_hash = data.archive_file.lambda.output_base64sha256
  runtime          = "nodejs22.x"
  architectures    = [var.lambda_architecture]
  timeout          = 10

  environment {
    variables = {
      MEDIA_BUCKET_NAME                   = var.media_s3_bucket_name
      MEDIA_DYNAMODB_TABLE_NAME           = var.dynamodb_table_name
      NODE_OPTIONS                        = "--require /var/task/instrumentation.js"
      OTEL_EXPORTER_OTLP_PROTOCOL         = "http/protobuf"
      OTEL_EXPORTER_OTLP_ENDPOINT         = var.otel_http_gateway_endpoint
      OTEL_NODE_DISABLED_INSTRUMENTATIONS = "fs,net,dns"
      OTEL_SEMCONV_STABILITY_OPT_IN       = "http"
    }
  }

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-manage-media-handler"
    }
  )
}

resource "aws_cloudwatch_log_group" "manage_media_cw_log_group" {
  depends_on        = [aws_lambda_function.manage_media]
  name              = "/aws/lambda/${aws_lambda_function.manage_media.function_name}"
  retention_in_days = 7

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-manage-media-handler-log-group"
    }
  )
}

resource "aws_iam_role_policy_attachment" "manage_lambda_iam_policy_policy_attachment" {
  role       = aws_iam_role.manage_media_iam_role.name
  policy_arn = aws_iam_policy.lambda_iam_policy.arn
}

resource "aws_lambda_event_source_mapping" "manage_media_sqs_event_source_mapping" {
  event_source_arn = var.media_management_sqs_queue_arn
  function_name    = aws_lambda_function.manage_media.arn

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-manage-media-sqs-event-source-mapping"
    }
  )
}

########################
# Process Media Lambda #
########################
resource "aws_vpc_security_group_egress_rule" "allow_process_lambda_outbound_traffic" {
  security_group_id = var.process_media_lambda_sg
  description       = "Allow all outbound traffic"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"

  tags = merge(var.additional_tags, {
    Name = "media-service-coll-allow-outbound-traffic-process-lambda"
  })
}

resource "aws_iam_role" "process_media_iam_role" {
  name               = "media-service-process-media-iam-role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-process-media-iam-role"
    }
  )
}

resource "aws_lambda_function" "process_media" {
  depends_on = [
    aws_lambda_layer_version.sharp_lambda_layer,
    aws_lambda_layer_version.otel_lambda_layer,
  ]
  layers = [
    aws_lambda_layer_version.sharp_lambda_layer.arn,
    aws_lambda_layer_version.otel_lambda_layer.arn
  ]

  vpc_config {
    security_group_ids = [var.process_media_lambda_sg]
    subnet_ids         = var.private_subnet_ids
  }

  filename         = local.lambda_zip_file
  function_name    = "media-service-process-media-handler"
  role             = aws_iam_role.process_media_iam_role.arn
  handler          = "index.handlers.processMediaUpload"
  source_code_hash = data.archive_file.lambda.output_base64sha256
  runtime          = "nodejs22.x"
  timeout          = 30

  # By having 1769 MB of memory, the function will be able to use 1 vCPU
  # https://docs.aws.amazon.com/lambda/latest/dg/gettingstarted-limits.html#compute-and-storage
  memory_size = 1769

  environment {
    variables = {
      MEDIA_BUCKET_NAME                   = var.media_s3_bucket_name
      MEDIA_DYNAMODB_TABLE_NAME           = var.dynamodb_table_name
      NODE_OPTIONS                        = "--require /var/task/instrumentation.js"
      OTEL_EXPORTER_OTLP_PROTOCOL         = "http/protobuf"
      OTEL_EXPORTER_OTLP_ENDPOINT         = var.otel_http_gateway_endpoint
      OTEL_NODE_DISABLED_INSTRUMENTATIONS = "fs,net,dns"
      OTEL_SEMCONV_STABILITY_OPT_IN       = "http"
    }
  }

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-process-media-handler"
    }
  )
}

resource "aws_cloudwatch_log_group" "process_media_cw_log_group" {
  depends_on        = [aws_lambda_function.process_media]
  name              = "/aws/lambda/${aws_lambda_function.process_media.function_name}"
  retention_in_days = 7

  tags = merge(
    var.additional_tags,
    {
      Name = "media-service-process-media-handler-log-group"
    }
  )
}

resource "aws_iam_role_policy_attachment" "process_lambda_iam_policy_policy_attachment" {
  role       = aws_iam_role.process_media_iam_role.name
  policy_arn = aws_iam_policy.lambda_iam_policy.arn
}

resource "aws_lambda_permission" "allow_bucket" {
  statement_id  = "AllowExecutionFromS3Bucket"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.process_media.arn
  principal     = "s3.amazonaws.com"
  source_arn    = var.media_bucket_arn
}

resource "aws_s3_bucket_notification" "media_bucket_notification" {
  bucket = var.media_bucket_id

  lambda_function {
    lambda_function_arn = aws_lambda_function.process_media.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "uploads/"
  }

  depends_on = [aws_lambda_permission.allow_bucket]
}
