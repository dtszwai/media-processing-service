output "media_bucket_arn" {
  value = aws_s3_bucket.media.arn
}

output "media_bucket_id" {
  value = aws_s3_bucket.media.id
}

output "media_bucket_name" {
  description = "Bucket name (matches Go S3_BUCKET env var)."
  value       = aws_s3_bucket.media.bucket
}

output "media_bucket_regional_domain_name" {
  value = aws_s3_bucket.media.bucket_regional_domain_name
}
