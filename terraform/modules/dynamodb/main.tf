data "aws_region" "current" {}

resource "aws_vpc_endpoint" "dynamo_db_endpoint" {
  vpc_id          = var.vpc_id
  service_name    = "com.amazonaws.${data.aws_region.current.name}.dynamodb"
  route_table_ids = var.private_route_table_ids

  tags = merge(var.additional_tags, {
    Name = "media-service-dynamodb-endpoint"
  })
}

data "aws_iam_policy_document" "dynamo_db_endpoint_policy" {
  statement {
    sid       = "DynamoDBEndpointPolicy"
    effect    = "Allow"
    actions   = ["dynamodb:*"]
    resources = ["*"]
    principals {
      type        = "AWS"
      identifiers = ["*"]
    }
  }
}

resource "aws_vpc_endpoint_policy" "dynamo_db_endpoint_policy" {
  vpc_endpoint_id = aws_vpc_endpoint.dynamo_db_endpoint.id
  policy          = data.aws_iam_policy_document.dynamo_db_endpoint_policy.json
}

resource "aws_dynamodb_table" "media_dynamo_table" {
  name         = var.dynamodb_table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  tags = merge(var.additional_tags, {
    Name = var.dynamodb_table_name
  })
}
