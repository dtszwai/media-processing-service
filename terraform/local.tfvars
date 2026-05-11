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

# LocalStack community does not support container-image Lambda
# functions, so the generation-worker stays on the zip-JAR runtime
# (no Python). To make the NotebookLM provider runnable in `make
# local-up`, the API container runs an in-process SQS poller that
# drains generation stage messages itself; the matching variable
# below also disables the Lambda's SQS event source mapping so we
# do not double-consume.
generation_worker_image_uri = ""
local_stage_poller_enabled  = true
