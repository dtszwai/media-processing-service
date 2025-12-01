# LocalStack configuration
# Usage: tflocal init -backend=false && tflocal apply -var-file=local.tfvars

is_local                   = true
aws_region                 = "us-west-2"
application_environment    = "localstack"
media_s3_bucket_name       = "media-bucket"
media_dynamo_table_name    = "media"
otel_exporter_endpoint     = "http://grafana:4318"
localstack_endpoint        = "http://localhost:4566"
localstack_lambda_endpoint = "http://localstack:4566"
