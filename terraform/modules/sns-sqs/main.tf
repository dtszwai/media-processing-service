resource "aws_sqs_queue" "media_management_sqs_queue" {
  name                      = var.media_mngmt_queue_name
  delay_seconds             = 10
  max_message_size          = 1024 * 5     # 5 KB
  message_retention_seconds = 60 * 60 * 24 # 1 day
  receive_wait_time_seconds = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.media_management_sqs_dlq.arn
    maxReceiveCount     = 5
  })

  # Six times the Lambda timeout (120s * 6 = 720s).
  # https://docs.aws.amazon.com/lambda/latest/dg/services-sqs-configure.html
  visibility_timeout_seconds = 720

  tags = merge(var.additional_tags, {
    Name = var.media_mngmt_queue_name
  })
}

resource "aws_sqs_queue" "media_management_sqs_dlq" {
  name = var.media_mngmt_dlq_name

  tags = merge(var.additional_tags, {
    Name = var.media_mngmt_dlq_name
  })
}

resource "aws_sns_topic" "media_management_topic" {
  name = var.media_mngmt_topic_name

  tags = merge(var.additional_tags, {
    Name = var.media_mngmt_topic_name
  })
}

data "aws_iam_policy_document" "sns_topic_policy" {
  statement {
    sid     = "SNSPublishToSQS"
    actions = ["sqs:SendMessage"]
    effect  = "Allow"

    principals {
      type        = "AWS"
      identifiers = ["*"]
    }

    resources = [
      aws_sqs_queue.media_management_sqs_queue.arn
    ]

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.media_management_topic.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "sqs_sns_topic_policy" {
  queue_url = aws_sqs_queue.media_management_sqs_queue.url
  policy    = data.aws_iam_policy_document.sns_topic_policy.json
}

# raw_message_delivery defaults to false; ManageMediaHandler expects SNS envelope.
# Do NOT normalize with the generation subscription below.
resource "aws_sns_topic_subscription" "sqs_sns_subscription" {
  topic_arn = aws_sns_topic.media_management_topic.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.media_management_sqs_queue.arn
}

resource "aws_sqs_queue" "generation_jobs_dlq" {
  name                      = "generation-jobs-dlq"
  message_retention_seconds = 1209600

  tags = merge(var.additional_tags, {
    Name = "generation-jobs-dlq"
  })
}

resource "aws_sqs_queue" "generation_jobs_queue" {
  name          = "generation-jobs"
  delay_seconds = 0
  # 16KB allows enhanced prompts and generation config in message body
  max_message_size          = 1024 * 16
  message_retention_seconds = 60 * 60 * 24
  receive_wait_time_seconds = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.generation_jobs_dlq.arn
    maxReceiveCount     = 5
  })

  # Six times the generation Lambda timeout (300s * 6 = 1800s).
  visibility_timeout_seconds = 1800

  tags = merge(var.additional_tags, {
    Name = "generation-jobs"
  })
}

resource "aws_sns_topic" "generation_jobs_topic" {
  name = "generation-jobs"

  tags = merge(var.additional_tags, {
    Name = "generation-jobs"
  })
}

data "aws_iam_policy_document" "generation_sns_topic_policy" {
  statement {
    sid     = "SNSPublishToGenerationSQS"
    actions = ["sqs:SendMessage"]
    effect  = "Allow"

    principals {
      type        = "AWS"
      identifiers = ["*"]
    }

    resources = [
      aws_sqs_queue.generation_jobs_queue.arn
    ]

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.generation_jobs_topic.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "generation_sqs_sns_topic_policy" {
  queue_url = aws_sqs_queue.generation_jobs_queue.url
  policy    = data.aws_iam_policy_document.generation_sns_topic_policy.json
}

resource "aws_sns_topic_subscription" "generation_sqs_sns_subscription" {
  topic_arn            = aws_sns_topic.generation_jobs_topic.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.generation_jobs_queue.arn
  raw_message_delivery = true
  # Per-tier queue routing (build-guide §5.3). Free queue catches the default "free" tier and
  # any message that omits the attribute (legacy or buggy publishers). Paid queue below filters
  # on tier = "paid" exclusively.
  filter_policy        = jsonencode({ tier = ["free"] })
  filter_policy_scope  = "MessageAttributes"
}

# =============================================================================
# Per-tier paid queue (build-guide §5.3 priority queues).
# =============================================================================
resource "aws_sqs_queue" "generation_paid_jobs_dlq" {
  name                      = "generation-jobs-paid-dlq"
  message_retention_seconds = 1209600

  tags = merge(var.additional_tags, {
    Name = "generation-jobs-paid-dlq"
  })
}

resource "aws_sqs_queue" "generation_paid_jobs_queue" {
  name                      = "generation-jobs-paid"
  delay_seconds             = 0
  max_message_size          = 1024 * 16
  message_retention_seconds = 60 * 60 * 24
  receive_wait_time_seconds = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.generation_paid_jobs_dlq.arn
    maxReceiveCount     = 5
  })
  visibility_timeout_seconds = 1800

  tags = merge(var.additional_tags, {
    Name = "generation-jobs-paid"
  })
}

data "aws_iam_policy_document" "generation_paid_sns_topic_policy" {
  statement {
    sid     = "SNSPublishToGenerationPaidSQS"
    actions = ["sqs:SendMessage"]
    effect  = "Allow"

    principals {
      type        = "AWS"
      identifiers = ["*"]
    }

    resources = [aws_sqs_queue.generation_paid_jobs_queue.arn]

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.generation_jobs_topic.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "generation_paid_sqs_sns_topic_policy" {
  queue_url = aws_sqs_queue.generation_paid_jobs_queue.url
  policy    = data.aws_iam_policy_document.generation_paid_sns_topic_policy.json
}

resource "aws_sns_topic_subscription" "generation_paid_sqs_sns_subscription" {
  topic_arn            = aws_sns_topic.generation_jobs_topic.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.generation_paid_jobs_queue.arn
  raw_message_delivery = true
  filter_policy        = jsonencode({ tier = ["paid"] })
  filter_policy_scope  = "MessageAttributes"
}

resource "aws_cloudwatch_metric_alarm" "generation_paid_dlq_not_empty" {
  count = var.dlq_alarm_enabled ? 1 : 0

  alarm_name          = "generation-paid-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Sum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Alarm when messages appear in the generation paid DLQ"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.generation_paid_jobs_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "generation-paid-dlq-alarm"
  })
}

# CloudWatch alarm for DLQ monitoring
resource "aws_cloudwatch_metric_alarm" "dlq_not_empty" {
  count = var.dlq_alarm_enabled ? 1 : 0

  alarm_name          = "media-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Sum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Alarm when messages appear in the DLQ"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.media_management_sqs_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "media-dlq-alarm"
  })
}

resource "aws_cloudwatch_metric_alarm" "generation_dlq_not_empty" {
  count = var.dlq_alarm_enabled ? 1 : 0

  alarm_name          = "generation-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Sum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Alarm when messages appear in the generation DLQ"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.generation_jobs_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "generation-dlq-alarm"
  })
}

resource "aws_cloudwatch_metric_alarm" "generation_queue_oldest_message_age" {
  count = var.dlq_alarm_enabled ? 1 : 0

  alarm_name          = "generation-queue-oldest-message-age"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 2
  metric_name         = "ApproximateAgeOfOldestMessage"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 1200
  alarm_description   = "SQS oldest message age exceeds SLA (4x Lambda timeout)"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.generation_jobs_queue.name
  }

  tags = merge(var.additional_tags, {
    Name = "generation-queue-oldest-message-age-alarm"
  })
}
