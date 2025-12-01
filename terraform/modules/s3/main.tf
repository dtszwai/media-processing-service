resource "aws_s3_bucket" "media_bucket" {
  bucket        = var.media_s3_bucket_name
  force_destroy = var.is_local

  tags = merge(var.additional_tags, {
    Name = var.media_s3_bucket_name
  })
}

resource "aws_s3_bucket_ownership_controls" "media_bucket_ownership_controls" {
  bucket = aws_s3_bucket.media_bucket.id
  rule {
    object_ownership = "BucketOwnerPreferred"
  }
}

resource "aws_s3_bucket_acl" "media_bucket_acl" {
  depends_on = [aws_s3_bucket_ownership_controls.media_bucket_ownership_controls]

  bucket = aws_s3_bucket.media_bucket.id
  acl    = "private"
}

resource "aws_s3_bucket_public_access_block" "media_bucket_public_access_block" {
  bucket = aws_s3_bucket.media_bucket.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
