# AWS Production configuration
# Usage: terraform init && terraform apply -var-file=prod.tfvars

is_local                = false
aws_region              = "us-west-2"
application_environment = "aws"
media_s3_bucket_name    = "media-bucket"
media_dynamo_table_name = "media"
otel_exporter_endpoint  = "http://localhost:4318"
