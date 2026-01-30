output "media_bucket_arn" {
  value = aws_s3_bucket.media_bucket.arn
}

output "media_bucket_id" {
  value = aws_s3_bucket.media_bucket.id
}

output "media_bucket_regional_domain_name" {
  value = aws_s3_bucket.media_bucket.bucket_regional_domain_name
}
