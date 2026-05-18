terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0, < 6.0"
    }
  }
}

data "aws_region" "current" {}

# LocalStack mocks VPC endpoints at the API level — they don't actually route
# DynamoDB traffic anywhere different, but creating them keeps the topology
# shape complete.
resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id          = var.vpc_id
  service_name    = "com.amazonaws.${data.aws_region.current.name}.dynamodb"
  route_table_ids = var.private_route_table_ids

  tags = merge(var.additional_tags, {
    Name = "${var.dynamodb_table_name}-vpc-endpoint"
  })
}

data "aws_iam_policy_document" "dynamodb_endpoint_policy" {
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

resource "aws_vpc_endpoint_policy" "dynamodb" {
  vpc_endpoint_id = aws_vpc_endpoint.dynamodb.id
  policy          = data.aws_iam_policy_document.dynamodb_endpoint_policy.json
}

# Single-table v2 layout matching backend/internal/infra/kv/dynamodb/schema.go.
# GSIs cover job lookup, tenant media feeds, lease expiry, upload lifecycle,
# audit entity/actor history, and asset role lookup.
# TTL on ttl_epoch. PITR is a no-op on LocalStack but kept on for fidelity.
resource "aws_dynamodb_table" "media" {
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
  attribute {
    name = "gsi_job_pk"
    type = "S"
  }
  attribute {
    name = "gsi_job_sk"
    type = "S"
  }
  attribute {
    name = "gsi_tenant_media_pk"
    type = "S"
  }
  attribute {
    name = "gsi_tenant_media_sk"
    type = "S"
  }
  attribute {
    name = "gsi_lease_pk"
    type = "S"
  }
  attribute {
    name = "gsi_lease_sk"
    type = "S"
  }
  attribute {
    name = "gsi_lifecycle_pk"
    type = "S"
  }
  attribute {
    name = "gsi_lifecycle_sk"
    type = "S"
  }
  attribute {
    name = "gsi_audit_entity_pk"
    type = "S"
  }
  attribute {
    name = "gsi_audit_entity_sk"
    type = "S"
  }
  attribute {
    name = "gsi_audit_actor_pk"
    type = "S"
  }
  attribute {
    name = "gsi_audit_actor_sk"
    type = "S"
  }
  attribute {
    name = "gsi_asset_role_pk"
    type = "S"
  }
  attribute {
    name = "gsi_asset_role_sk"
    type = "S"
  }

  global_secondary_index {
    name            = "gsi_job"
    hash_key        = "gsi_job_pk"
    range_key       = "gsi_job_sk"
    projection_type = "ALL"
  }
  global_secondary_index {
    name            = "gsi_tenant_media"
    hash_key        = "gsi_tenant_media_pk"
    range_key       = "gsi_tenant_media_sk"
    projection_type = "ALL"
  }
  global_secondary_index {
    name            = "gsi_lease_expiry"
    hash_key        = "gsi_lease_pk"
    range_key       = "gsi_lease_sk"
    projection_type = "ALL"
  }
  global_secondary_index {
    name            = "gsi_lifecycle"
    hash_key        = "gsi_lifecycle_pk"
    range_key       = "gsi_lifecycle_sk"
    projection_type = "ALL"
  }
  # gsi_audit_entity: per-entity history (e.g. all events for one API key).
  # gsi_audit_actor: per-actor activity (e.g. all admin actions by user X).
  global_secondary_index {
    name            = "gsi_audit_entity"
    hash_key        = "gsi_audit_entity_pk"
    range_key       = "gsi_audit_entity_sk"
    projection_type = "ALL"
  }
  global_secondary_index {
    name            = "gsi_audit_actor"
    hash_key        = "gsi_audit_actor_pk"
    range_key       = "gsi_audit_actor_sk"
    projection_type = "ALL"
  }
  # gsi_asset_role: role-keyed selector index for /preview-url, /thumbnail-url,
  # /download-url. Range key sorts by (priority, created_at) so the highest-
  # priority COMPLETE asset of a given role is the first hit on a Limit=1 query.
  global_secondary_index {
    name            = "gsi_asset_role"
    hash_key        = "gsi_asset_role_pk"
    range_key       = "gsi_asset_role_sk"
    projection_type = "ALL"
  }

  ttl {
    attribute_name = "ttl_epoch"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = merge(var.additional_tags, {
    Name = var.dynamodb_table_name
  })
}
