terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.84"
    }
  }
}

locals {
  common_tags = {
    App = "media-service"
  }
}

resource "aws_s3_bucket" "terraform_remote_state_bucket" {
  bucket = "media-service-terraform-state-bucket"

  tags = merge(local.common_tags, {
    Name = "media-service-terraform-state-bucket"
  })
}

resource "aws_s3_bucket_versioning" "terraform_remote_state_bucket_versioning" {
  bucket = aws_s3_bucket.terraform_remote_state_bucket.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_dynamodb_table" "terraform_remote_state_lock_table" {
  name           = "media-service-terraform-state-lock-table"
  read_capacity  = 1
  write_capacity = 1
  hash_key       = "LockID"
  attribute {
    name = "LockID"
    type = "S"
  }

  depends_on = [aws_s3_bucket.terraform_remote_state_bucket]

  tags = merge(local.common_tags, {
    Name = "media-service-terraform-state-lock-table"
  })
}
