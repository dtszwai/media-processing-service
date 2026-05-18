terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0, < 6.0"
    }
  }
}

data "aws_caller_identity" "current" {}

# =============================================================================
# media-management-topic + media-jobs queue.
# Single concern: image derivation + soft-delete fanout.
# =============================================================================

resource "aws_sns_topic" "media" {
  name = "${var.name_prefix}-media-management-topic"

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-management-topic"
  })
}

resource "aws_sqs_queue" "media_dlq" {
  name                      = "${var.name_prefix}-media-jobs-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-jobs-dlq"
  })
}

resource "aws_sqs_queue" "media" {
  name                       = "${var.name_prefix}-media-jobs"
  visibility_timeout_seconds = 120
  message_retention_seconds  = 86400 # 1 day
  receive_wait_time_seconds  = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.media_dlq.arn
    maxReceiveCount     = 5
  })

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-jobs"
  })
}

# raw_message_delivery=false → SNS envelope reaches the worker. The Go
# media-worker decodes the envelope before applying the MediaEvent.
resource "aws_sns_topic_subscription" "media_to_queue" {
  topic_arn            = aws_sns_topic.media.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.media.arn
  raw_message_delivery = false
}

resource "aws_sqs_queue_policy" "media" {
  queue_url = aws_sqs_queue.media.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.media.arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_sns_topic.media.arn }
      }
    }]
  })
}

# =============================================================================
# media-cleanup-topic + media-cleanup queue.
# Single concern: S3 object deletion + asset-row DELETED flip after soft-delete
# or failed-upload rejection.
# =============================================================================

resource "aws_sns_topic" "media_cleanup" {
  name = "${var.name_prefix}-media-cleanup-topic"

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-cleanup-topic"
  })
}

resource "aws_sqs_queue" "media_cleanup_dlq" {
  name                      = "${var.name_prefix}-media-cleanup-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-cleanup-dlq"
  })
}

resource "aws_sqs_queue" "media_cleanup" {
  name                       = "${var.name_prefix}-media-cleanup"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 86400 # 1 day
  receive_wait_time_seconds  = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.media_cleanup_dlq.arn
    maxReceiveCount     = 5
  })

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-cleanup"
  })
}

resource "aws_sns_topic_subscription" "media_cleanup_to_queue" {
  topic_arn            = aws_sns_topic.media_cleanup.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.media_cleanup.arn
  raw_message_delivery = false
}

resource "aws_sqs_queue_policy" "media_cleanup" {
  queue_url = aws_sqs_queue.media_cleanup.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.media_cleanup.arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_sns_topic.media_cleanup.arn }
      }
    }]
  })
}

# DLQ depth alarms use AWS/SQS ApproximateNumberOfMessagesVisible — emitted by
# AWS (and LocalStack) for every queue, no app instrumentation needed. The
# alarm resource shape, threshold, and dashboard wiring matter more than
# whether LocalStack actually triggers SNS actions on breach.
resource "aws_cloudwatch_metric_alarm" "media_cleanup_dlq_not_empty" {
  alarm_name          = "${var.name_prefix}-media-cleanup-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Messages visible in the media-cleanup DLQ for 5m — no DLQ should be normal."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.media_cleanup_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-cleanup-dlq-alarm"
  })
}

# =============================================================================
# generation-jobs topic + per-tier × resource-class queues with filter
# policies (tier ∈ {FREE, PAID} × listed resource classes).
# =============================================================================

resource "aws_sns_topic" "generation" {
  name = "${var.name_prefix}-generation-jobs"

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-generation-jobs"
  })
}

locals {
  resource_classes = [
    "FAST",
    "PROVIDER",
    "POLL",
    "IMAGE_PROCESS",
  ]
  tiers = ["FREE", "PAID"]
  queue_pairs = [for t in local.tiers : {
    for c in local.resource_classes : "${lower(t)}-${replace(lower(c), "_", "-")}" => {
      tier           = t
      resource_class = c
    }
  }]
  generation_queues = merge(local.queue_pairs...)
}

resource "aws_sqs_queue" "generation_dlq" {
  for_each = local.generation_queues

  name                      = "generation-jobs-${each.key}-dlq"
  message_retention_seconds = 1209600

  tags = merge(var.additional_tags, {
    Name = "generation-jobs-${each.key}-dlq"
  })
}

resource "aws_sqs_queue" "generation" {
  for_each = local.generation_queues

  name                       = "generation-jobs-${each.key}"
  visibility_timeout_seconds = 1800 # 6x the 300s generation Lambda timeout
  message_retention_seconds  = 86400
  max_message_size           = 16384
  receive_wait_time_seconds  = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.generation_dlq[each.key].arn
    maxReceiveCount     = 5
  })

  tags = merge(var.additional_tags, {
    Name          = "generation-jobs-${each.key}"
    Tier          = each.value.tier
    ResourceClass = each.value.resource_class
  })
}

resource "aws_sns_topic_subscription" "generation" {
  for_each = local.generation_queues

  topic_arn            = aws_sns_topic.generation.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.generation[each.key].arn
  raw_message_delivery = true
  filter_policy = jsonencode({
    tier           = [each.value.tier]
    resource_class = [each.value.resource_class]
  })
  filter_policy_scope = "MessageAttributes"
}

resource "aws_sqs_queue_policy" "generation" {
  for_each = local.generation_queues

  queue_url = aws_sqs_queue.generation[each.key].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.generation[each.key].arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_sns_topic.generation.arn }
      }
    }]
  })
}

# =============================================================================
# analytics-events topic + analytics-tracker queue + DLQ.
# Single concern: fan-out of view/download events from the API to the
# analytics-worker Lambda that writes DDB counters via Sink.Apply.
# =============================================================================

resource "aws_sns_topic" "analytics_events" {
  name = "${var.name_prefix}-analytics-events"

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-analytics-events"
  })
}

resource "aws_sqs_queue" "analytics_tracker_dlq" {
  name                      = "${var.name_prefix}-analytics-tracker-dlq"
  message_retention_seconds = 1209600 # 14 days — matches LedgerTTL

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-analytics-tracker-dlq"
  })
}

resource "aws_sqs_queue" "analytics_tracker" {
  name                       = "${var.name_prefix}-analytics-tracker"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400 # 1 day
  receive_wait_time_seconds  = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.analytics_tracker_dlq.arn
    maxReceiveCount     = 5
  })

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-analytics-tracker"
  })
}

resource "aws_sns_topic_subscription" "analytics_tracker" {
  topic_arn            = aws_sns_topic.analytics_events.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.analytics_tracker.arn
  raw_message_delivery = false
}

resource "aws_sqs_queue_policy" "analytics_tracker" {
  queue_url = aws_sqs_queue.analytics_tracker.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.analytics_tracker.arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_sns_topic.analytics_events.arn }
      }
    }]
  })
}

resource "aws_cloudwatch_metric_alarm" "analytics_tracker_dlq_not_empty" {
  alarm_name          = "${var.name_prefix}-analytics-tracker-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Messages visible in the analytics-tracker DLQ for 5m — no DLQ should be normal."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.analytics_tracker_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-analytics-tracker-dlq-alarm"
  })
}

# =============================================================================
# media-upload-events queue — fed by S3 ObjectCreated notifications on the
# media bucket, drained by the upload-events-worker. The worker calls the same
# idempotent completion handler the REST /upload/complete endpoint uses, so
# uploads converge even when the client never returns to call Complete.
#
# No SNS topic on this lane: S3 publishes straight to SQS. The queue policy
# allows the bucket as source; the SourceAccount + SourceArn conditions stop
# any other principal from injecting messages.
# =============================================================================

resource "aws_sqs_queue" "media_upload_events_dlq" {
  name                      = "${var.name_prefix}-media-upload-events-dlq"
  message_retention_seconds = 1209600 # 14 days

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-upload-events-dlq"
  })
}

resource "aws_sqs_queue" "media_upload_events" {
  name                       = "${var.name_prefix}-media-upload-events"
  visibility_timeout_seconds = 120
  message_retention_seconds  = 86400 # 1 day
  receive_wait_time_seconds  = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.media_upload_events_dlq.arn
    maxReceiveCount     = 5
  })

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-upload-events"
  })
}

# IAM permission the bucket needs to publish into this queue. SourceArn ties
# the grant to one specific bucket so a misrouted notification config from
# elsewhere can never enqueue here. The bucket ARN is reconstructed from the
# bucket name to break the would-be module cycle between s3 and sns-sqs.
resource "aws_sqs_queue_policy" "media_upload_events" {
  queue_url = aws_sqs_queue.media_upload_events.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "s3.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.media_upload_events.arn
      Condition = {
        ArnEquals    = { "aws:SourceArn" = "arn:aws:s3:::${var.media_bucket_name}" }
        StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.current.account_id }
      }
    }]
  })
}

resource "aws_cloudwatch_metric_alarm" "media_upload_events_dlq_not_empty" {
  alarm_name          = "${var.name_prefix}-media-upload-events-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Messages visible in the media-upload-events DLQ for 5m — no DLQ should be normal."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.media_upload_events_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-upload-events-dlq-alarm"
  })
}

# =============================================================================
# webhook-delivery queue — direct enqueue from media-worker, no SNS.
# =============================================================================

resource "aws_sqs_queue" "webhook_dlq" {
  name                      = "${var.name_prefix}-webhook-delivery-dlq"
  message_retention_seconds = 1209600

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-webhook-delivery-dlq"
  })
}

resource "aws_sqs_queue" "webhook" {
  name                       = "${var.name_prefix}-webhook-delivery"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 86400
  receive_wait_time_seconds  = 5
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.webhook_dlq.arn
    maxReceiveCount     = 5
  })

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-webhook-delivery"
  })
}

# =============================================================================
# DLQ depth alarms. Threshold is breach-on-first-message, sustained 5m to avoid
# flapping on transient redrives.
# =============================================================================

resource "aws_cloudwatch_metric_alarm" "media_dlq_not_empty" {
  alarm_name          = "${var.name_prefix}-media-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Messages visible in the media-jobs DLQ for 5m — no DLQ should be normal."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.media_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-media-dlq-alarm"
  })
}

resource "aws_cloudwatch_metric_alarm" "generation_dlq_not_empty" {
  for_each = local.generation_queues

  alarm_name          = "${var.name_prefix}-generation-jobs-${each.key}-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Messages visible in the ${each.key} generation DLQ for 5m — no DLQ should be normal."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.generation_dlq[each.key].name
  }

  tags = merge(var.additional_tags, {
    Name          = "${var.name_prefix}-generation-jobs-${each.key}-dlq-alarm"
    Tier          = each.value.tier
    ResourceClass = each.value.resource_class
  })
}

resource "aws_cloudwatch_metric_alarm" "webhook_dlq_not_empty" {
  alarm_name          = "${var.name_prefix}-webhook-dlq-not-empty"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = var.dlq_alarm_threshold
  alarm_description   = "Messages visible in the webhook-delivery DLQ for 5m — no DLQ should be normal."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.webhook_dlq.name
  }

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-webhook-dlq-alarm"
  })
}

# =============================================================================
# Workflow queue oldest-age alarm — uses AWS/SQS ApproximateAgeOfOldestMessage,
# scoped to each generation queue. Crossing the stage SLA means workers are
# stuck or under-provisioned. Threshold: 600s (10 min) is a coarse upper bound
# spanning every stage SLA in genworkflow; finer per-stage SLAs would require
# the per-stage queue_age_ms histogram the Go code does not yet emit.
# =============================================================================

resource "aws_cloudwatch_metric_alarm" "generation_queue_age" {
  for_each = local.generation_queues

  alarm_name          = "${var.name_prefix}-generation-jobs-${each.key}-oldest-age"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateAgeOfOldestMessage"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 600
  alarm_description   = "Oldest message in the ${each.key} generation queue older than stage SLA — workers stuck or under-provisioned."
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.generation[each.key].name
  }

  tags = merge(var.additional_tags, {
    Name          = "${var.name_prefix}-generation-jobs-${each.key}-oldest-age-alarm"
    Tier          = each.value.tier
    ResourceClass = each.value.resource_class
  })
}
