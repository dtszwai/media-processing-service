data "aws_region" "current" {}

# =============================================================================
# Load Balancer
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

  health_check {
    protocol            = "HTTP"
    port                = var.app_port
    path                = "/v1/media/health"
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
# ECS Cluster
# =============================================================================

resource "aws_ecs_cluster" "main" {
  name = "media-service-cluster"

  tags = merge(var.additional_tags, {
    Name = "media-service-cluster"
  })
}

# =============================================================================
# Container Security
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
# IAM Roles
# =============================================================================

data "aws_iam_policy_document" "ecs_assume_role" {
  statement {
    sid     = "ECSAssumeRole"
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
  path               = "/"

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
      "ecr:GetDownloadUrlForLayer"
    ]
    resources = [var.ecr_repository_arn]
  }

  statement {
    sid    = "S3"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:ListBucket"
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
    sid       = "SNS"
    effect    = "Allow"
    actions   = ["sns:Publish"]
    resources = [var.media_management_topic_arn]
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
# ECS Task Definition
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
      name      = "api"
      image     = var.api_image_uri
      essential = true
      environment = [
        { name = "APP_PORT", value = tostring(var.app_port) },
        { name = "MEDIA_BUCKET_NAME", value = var.media_s3_bucket_name },
        { name = "MEDIA_DYNAMODB_TABLE_NAME", value = var.dynamodb_table_name },
        { name = "MEDIA_MANAGEMENT_TOPIC_ARN", value = var.media_management_topic_arn },
        { name = "OTEL_EXPORTER_OTLP_ENDPOINT", value = var.otel_exporter_endpoint },
        { name = "OTEL_SERVICE_NAME", value = "media-service" }
      ]
      portMappings = [
        {
          protocol      = "tcp"
          containerPort = var.app_port
          hostPort      = var.app_port
        }
      ]
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
# ECS Service
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
    aws_alb_target_group.api
  ]

  tags = merge(var.additional_tags, {
    Name = "api-service"
  })
}
