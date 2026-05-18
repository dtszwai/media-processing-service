terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0, < 6.0"
    }
  }
}

resource "aws_s3_bucket" "media" {
  bucket = var.media_s3_bucket_name

  # force_destroy is fine here — this config only ever targets LocalStack, so
  # `terraform destroy` should always succeed without manual bucket emptying.
  force_destroy = true

  tags = merge(var.additional_tags, {
    Name = var.media_s3_bucket_name
  })
}

resource "aws_s3_bucket_versioning" "media" {
  bucket = aws_s3_bucket.media.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    object_ownership = "BucketOwnerPreferred"
  }
}

resource "aws_s3_bucket_acl" "media" {
  depends_on = [aws_s3_bucket_ownership_controls.media]
  bucket     = aws_s3_bucket.media.id
  acl        = "private"
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket                  = aws_s3_bucket.media.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_cors_configuration" "media" {
  bucket = aws_s3_bucket.media.id

  cors_rule {
    allowed_headers = ["*"]
    allowed_methods = ["GET", "PUT", "POST", "DELETE", "HEAD"]
    allowed_origins = ["*"]
    expose_headers  = ["ETag"]
    max_age_seconds = 3600
  }
}

# S3 ObjectCreated → SQS notification. This is the failsafe completion path: the
# upload-events worker drains the queue and runs the same idempotent completion
# FSM as /upload/complete, so a client crashing after PUT but before calling
# Complete still gets the row promoted.
#
# The queue ARN flows in as a variable rather than via a sibling module
# reference because the queue's policy depends on the bucket ARN (chicken-
# and-egg if both lived in their own modules referencing each other). At plan
# time the queue is created first (sns-sqs module), then this notification
# wires it to the bucket — a one-direction edge between modules.
resource "aws_s3_bucket_notification" "media" {
  bucket = aws_s3_bucket.media.id

  queue {
    queue_arn = var.media_upload_events_queue_arn
    events    = ["s3:ObjectCreated:*"]
  }
}
