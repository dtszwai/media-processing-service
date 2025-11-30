#!/bin/bash
set -euo pipefail

# ============================================================
# LocalStack Resource Initialization
# Creates AWS resources for local development
# ============================================================

# These values should match terraform.tfvars
AWS_REGION="us-west-2"
BUCKET_NAME="media-bucket"
TABLE_NAME="media"
TOPIC_NAME="media-management-topic"
QUEUE_NAME="media-management-sqs-queue"
LAMBDA_NAME="media-service-manage-media-handler"
LAMBDA_JAR_PATH="/opt/lambdas/media-service-lambdas-1.0.0.jar"

log() {
    echo "[init] $1"
}

create_s3_bucket() {
    awslocal s3 mb "s3://${BUCKET_NAME}" --region "${AWS_REGION}"
    log "S3 bucket created: ${BUCKET_NAME}"
}

create_dynamodb_table() {
    awslocal dynamodb create-table \
        --table-name "${TABLE_NAME}" \
        --attribute-definitions \
            AttributeName=PK,AttributeType=S \
            AttributeName=SK,AttributeType=S \
            AttributeName=createdAt,AttributeType=S \
        --key-schema \
            AttributeName=PK,KeyType=HASH \
            AttributeName=SK,KeyType=RANGE \
        --global-secondary-indexes \
            'IndexName=SK-createdAt-index,KeySchema=[{AttributeName=SK,KeyType=HASH},{AttributeName=createdAt,KeyType=RANGE}],Projection={ProjectionType=ALL}' \
        --billing-mode PAY_PER_REQUEST \
        --region "${AWS_REGION}"
    log "DynamoDB table created: ${TABLE_NAME}"
}

create_sns_topic() {
    awslocal sns create-topic \
        --name "${TOPIC_NAME}" \
        --region "${AWS_REGION}"
    log "SNS topic created: ${TOPIC_NAME}"
}

create_sqs_queue() {
    # Visibility timeout = 6x Lambda timeout (30s * 6 = 180s)
    awslocal sqs create-queue \
        --queue-name "${QUEUE_NAME}" \
        --attributes "VisibilityTimeout=180" \
        --region "${AWS_REGION}"
    log "SQS queue created: ${QUEUE_NAME}"
}

subscribe_sqs_to_sns() {
    local topic_arn queue_arn queue_url

    topic_arn=$(awslocal sns list-topics --region "${AWS_REGION}" \
        --query "Topics[?ends_with(TopicArn, ':${TOPIC_NAME}')].TopicArn" \
        --output text)

    queue_url="http://localhost:4566/000000000000/${QUEUE_NAME}"
    queue_arn=$(awslocal sqs get-queue-attributes \
        --queue-url "${queue_url}" \
        --attribute-names QueueArn \
        --query 'Attributes.QueueArn' \
        --output text \
        --region "${AWS_REGION}")

    awslocal sns subscribe \
        --topic-arn "${topic_arn}" \
        --protocol sqs \
        --notification-endpoint "${queue_arn}" \
        --region "${AWS_REGION}"
    log "SQS subscribed to SNS topic"
}

create_lambda_function() {
    # Check if Lambda JAR exists
    if [ ! -f "${LAMBDA_JAR_PATH}" ]; then
        log "WARNING: Lambda JAR not found at ${LAMBDA_JAR_PATH}"
        log "Please build the lambdas first: cd app/lambdas && mvn package"
        return 1
    fi

    # Create IAM role for Lambda (LocalStack auto-creates if not exists)
    awslocal iam create-role \
        --role-name lambda-execution-role \
        --assume-role-policy-document '{"Version": "2012-10-17","Statement": [{"Effect": "Allow","Principal": {"Service": "lambda.amazonaws.com"},"Action": "sts:AssumeRole"}]}' \
        --region "${AWS_REGION}" 2>/dev/null || true

    # Create Lambda function (matching Terraform config)
    # Use host.docker.internal to allow Lambda container to reach LocalStack
    awslocal lambda create-function \
        --function-name "${LAMBDA_NAME}" \
        --runtime java21 \
        --handler "com.mediaservice.lambda.ManageMediaHandler::handleRequest" \
        --role "arn:aws:iam::000000000000:role/lambda-execution-role" \
        --zip-file "fileb://${LAMBDA_JAR_PATH}" \
        --timeout 30 \
        --memory-size 1024 \
        --environment "Variables={AWS_REGION=${AWS_REGION},MEDIA_BUCKET_NAME=${BUCKET_NAME},MEDIA_DYNAMODB_TABLE_NAME=${TABLE_NAME},AWS_S3_ENDPOINT=http://host.docker.internal:4566,AWS_DYNAMODB_ENDPOINT=http://host.docker.internal:4566,OTEL_EXPORTER_OTLP_ENDPOINT=http://grafana-lgtm:4318,OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf,OTEL_SERVICE_NAME=media-service-lambda,OTEL_LOGS_EXPORTER=otlp}" \
        --region "${AWS_REGION}"
    log "Lambda function created: ${LAMBDA_NAME}"
}

configure_sqs_lambda_trigger() {
    local queue_arn queue_url

    queue_url="http://localhost:4566/000000000000/${QUEUE_NAME}"
    queue_arn=$(awslocal sqs get-queue-attributes \
        --queue-url "${queue_url}" \
        --attribute-names QueueArn \
        --query 'Attributes.QueueArn' \
        --output text \
        --region "${AWS_REGION}")

    # Create event source mapping (SQS -> Lambda)
    awslocal lambda create-event-source-mapping \
        --function-name "${LAMBDA_NAME}" \
        --event-source-arn "${queue_arn}" \
        --batch-size 1 \
        --region "${AWS_REGION}"
    log "SQS trigger configured for Lambda"
}

main() {
    log "Starting LocalStack initialization..."

    create_s3_bucket
    create_dynamodb_table
    create_sns_topic
    create_sqs_queue
    subscribe_sqs_to_sns
    create_lambda_function
    configure_sqs_lambda_trigger

    log "Initialization complete"
}

main "$@"
