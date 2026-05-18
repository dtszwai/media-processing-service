data "aws_region" "current" {}

# =============================================================================
# ALB
# =============================================================================

resource "aws_vpc_security_group_ingress_rule" "alb_inbound" {
  security_group_id = var.alb_sg_id
  description       = "Allow HTTP traffic"
  from_port         = 80
  to_port           = 80
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"

  tags = merge(var.additional_tags, {
    Name = "api-alb-inbound"
  })
}

resource "aws_vpc_security_group_egress_rule" "alb_outbound" {
  security_group_id = var.alb_sg_id
  description       = "Allow all outbound traffic"
  from_port         = 0
  to_port           = 65535
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"

  tags = merge(var.additional_tags, {
    Name = "api-alb-outbound"
  })
}

resource "aws_alb" "api" {
  name            = "api-alb"
  subnets         = var.public_subnet_ids
  security_groups = [var.alb_sg_id]

  tags = merge(var.additional_tags, {
    Name = "api-alb"
  })
}

resource "aws_alb_target_group" "api" {
  name        = "api-target-group"
  port        = var.app_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  # Deregistration timeout matches the container stopTimeout below so
  # in-flight requests can drain during rolling deploys.
  deregistration_delay = 30

  # /healthz is the Go api's liveness endpoint (cmd/api/main.go).
  health_check {
    protocol            = "HTTP"
    port                = var.app_port
    path                = "/healthz"
    interval            = 10
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 2
  }

  tags = merge(var.additional_tags, {
    Name = "api-target-group"
  })
}

resource "aws_alb_listener" "api" {
  load_balancer_arn = aws_alb.api.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    target_group_arn = aws_alb_target_group.api.arn
    type             = "forward"
  }

  depends_on = [aws_alb_target_group.api]

  tags = merge(var.additional_tags, {
    Name = "api-alb-listener"
  })
}

# =============================================================================
# ECS cluster
# =============================================================================

resource "aws_ecs_cluster" "main" {
  name = "media-service-cluster"

  tags = merge(var.additional_tags, {
    Name = "media-service-cluster"
  })
}

# =============================================================================
# Container SG
# =============================================================================

resource "aws_vpc_security_group_ingress_rule" "container_inbound" {
  security_group_id            = var.container_sg_id
  referenced_security_group_id = var.alb_sg_id
  description                  = "Allow traffic from ALB"
  from_port                    = var.app_port
  to_port                      = var.app_port
  ip_protocol                  = "tcp"

  tags = merge(var.additional_tags, {
    Name = "api-container-inbound"
  })
}

resource "aws_vpc_security_group_egress_rule" "container_outbound" {
  security_group_id = var.container_sg_id
  description       = "Allow all outbound traffic"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"

  tags = merge(var.additional_tags, {
    Name = "api-container-outbound"
  })
}

# =============================================================================
# IAM
# =============================================================================

data "aws_iam_policy_document" "ecs_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task_role" {
  name               = "api-task-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume_role.json

  tags = merge(var.additional_tags, {
    Name = "api-task-role"
  })
}

data "aws_iam_policy_document" "task_policy" {
  statement {
    sid    = "ECR"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:BatchGetImage",
      "ecr:GetAuthorizationToken",
      "ecr:GetDownloadUrlForLayer",
    ]
    resources = [var.ecr_repository_arn]
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
    ]
  }

  statement {
    sid     = "SNSPublish"
    effect  = "Allow"
    actions = ["sns:Publish"]
    resources = [
      var.media_topic_arn,
      var.generation_topic_arn,
    ]
  }

  # API enqueues stage messages directly when running the in-process poller
  # path. Otherwise the queue ARNs are only inspected for backpressure.
  statement {
    sid    = "SQSGenerationManage"
    effect = "Allow"
    actions = [
      "sqs:SendMessage",
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility",
    ]
    resources = concat(values(var.generation_queue_arns), [var.webhook_queue_arn, var.media_queue_arn])
  }

  # KMS data-key wrapping for prompt envelope encryption (AES-256-GCM, see
  # internal/infra/sealer/impl/kms). LocalStack skips this — kms_prompt_key_arn
  # is empty there and the statement is omitted.
  dynamic "statement" {
    for_each = length(var.kms_prompt_key_arn) > 0 ? [1] : []
    content {
      sid    = "KMSPromptEnvelope"
      effect = "Allow"
      actions = [
        "kms:GenerateDataKey",
        "kms:Decrypt",
      ]
      resources = [var.kms_prompt_key_arn]
    }
  }
}

resource "aws_iam_role_policy" "task_policy" {
  name   = "api-task-policy"
  role   = aws_iam_role.task_role.id
  policy = data.aws_iam_policy_document.task_policy.json
}

resource "aws_iam_role" "execution_role" {
  name               = "api-execution-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume_role.json

  tags = merge(var.additional_tags, {
    Name = "api-execution-role"
  })
}

resource "aws_iam_role_policy_attachment" "execution_role_policy" {
  role       = aws_iam_role.execution_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# =============================================================================
# Task definition
# =============================================================================

resource "aws_ecs_task_definition" "api" {
  family                   = "media-service-api"
  execution_role_arn       = aws_iam_role.execution_role.arn
  task_role_arn            = aws_iam_role.task_role.arn
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "512"
  memory                   = "1024"

  container_definitions = jsonencode([
    {
      name        = "api"
      image       = var.api_image_uri
      essential   = true
      stopTimeout = 30
      # Container command — matches deploy/compose/Dockerfile.api ENTRYPOINT.
      command = ["/usr/local/bin/api"]

      environment = [
        # API listener.
        { name = "API_HTTP_ADDR", value = ":${var.app_port}" },

        # AWS / SDK.
        { name = "AWS_REGION", value = data.aws_region.current.name },

        # Bootstrap config. The ARN / URL set is all-or-none; partial managed
        # topology fails closed at startup.
        { name = "S3_BUCKET", value = var.media_s3_bucket_name },
        { name = "DDB_TABLE", value = var.dynamodb_table_name },
        { name = "SNS_MEDIA_TOPIC", value = var.media_topic_name },
        { name = "SNS_MEDIA_TOPIC_ARN", value = var.media_topic_arn },
        { name = "SNS_MEDIA_CLEANUP_TOPIC", value = var.media_cleanup_topic_name },
        { name = "SNS_MEDIA_CLEANUP_TOPIC_ARN", value = var.media_cleanup_topic_arn },
        { name = "SNS_GENERATION_TOPIC", value = var.generation_topic_name },
        { name = "SNS_GENERATION_TOPIC_ARN", value = var.generation_topic_arn },
        { name = "SNS_ANALYTICS_TOPIC", value = var.analytics_events_topic_name },
        { name = "SNS_ANALYTICS_TOPIC_ARN", value = var.analytics_events_topic_arn },
        { name = "KMS_PROMPT_KEY_ID", value = var.kms_prompt_key_id },
        { name = "SQS_MEDIA_QUEUE", value = var.media_queue_name },
        { name = "SQS_MEDIA_QUEUE_URL", value = var.media_queue_url },
        { name = "SQS_MEDIA_CLEANUP_QUEUE", value = var.media_cleanup_queue_name },
        { name = "SQS_MEDIA_CLEANUP_QUEUE_URL", value = var.media_cleanup_queue_url },
        { name = "SQS_MEDIA_UPLOAD_EVENTS_QUEUE", value = var.media_upload_events_queue_name },
        { name = "SQS_MEDIA_UPLOAD_EVENTS_QUEUE_URL", value = var.media_upload_events_queue_url },
        { name = "SQS_WEBHOOK_QUEUE", value = var.webhook_queue_name },
        { name = "SQS_WEBHOOK_QUEUE_URL", value = var.webhook_queue_url },
        { name = "SQS_ANALYTICS_QUEUE", value = var.analytics_tracker_queue_name },
        { name = "SQS_ANALYTICS_QUEUE_URL", value = var.analytics_tracker_queue_url },
        { name = "SQS_GENERATION_QUEUE_URLS", value = jsonencode({ for key, url in var.generation_queue_urls : "generation-jobs-${key}" => url }) },

        # Telemetry. OTel collector endpoint + service name stay in env;
        # log level, sampler, env name now come from conf/app.
        { name = "OTEL_EXPORTER_OTLP_ENDPOINT", value = var.otel_exporter_endpoint },
        { name = "OTEL_SERVICE_NAME", value = "media-service-api" },

        # MSG_ENV selects the embedded YAML overlay.
        { name = "MSG_ENV", value = var.application_environment },

        # Webhook signing secret. Real prod values come from Secrets Manager;
        # the var default is scoped to local development.
        { name = "WEBHOOK_SECRET", value = var.webhook_secret },
      ]

      portMappings = [{
        protocol      = "tcp"
        containerPort = var.app_port
        hostPort      = var.app_port
      }]

      healthCheck = {
        command     = ["CMD-SHELL", "wget -qO- http://127.0.0.1:${var.app_port}/healthz || exit 1"]
        interval    = 15
        timeout     = 5
        retries     = 3
        startPeriod = 30
      }

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = var.app_log_group_name
          awslogs-region        = data.aws_region.current.name
          awslogs-stream-prefix = "api"
        }
      }
    }
  ])

  tags = merge(var.additional_tags, {
    Name = "media-service-api-task"
  })
}

# =============================================================================
# Service
# =============================================================================

resource "aws_ecs_service" "api" {
  name            = "api-service"
  cluster         = aws_ecs_cluster.main.arn
  task_definition = aws_ecs_task_definition.api.arn
  launch_type     = "FARGATE"
  desired_count   = var.desired_task_count

  load_balancer {
    target_group_arn = aws_alb_target_group.api.arn
    container_name   = "api"
    container_port   = var.app_port
  }

  network_configuration {
    assign_public_ip = false
    subnets          = var.private_subnet_ids
    security_groups  = [var.container_sg_id]
  }

  depends_on = [
    aws_ecs_cluster.main,
    aws_ecs_task_definition.api,
    aws_alb_target_group.api,
  ]

  tags = merge(var.additional_tags, {
    Name = "api-service"
  })
}
