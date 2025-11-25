data "aws_region" "current" {}

resource "aws_ecr_repository" "ecr_repository" {
  name = var.ecr_repository_name

  tags = merge(var.additional_tags, {
    Name = var.ecr_repository_name
  })
}

resource "aws_ecr_lifecycle_policy" "ecr_repository_lifecycle_policy" {
  repository = aws_ecr_repository.ecr_repository.name
  policy     = <<EOF
{
    "rules": [
        {
            "rulePriority": 1,
            "description": "Keep only the last x images, expire all others",
            "selection": {
                "tagStatus": "any",
                "countType": "imageCountMoreThan",
                "countNumber": 10
            },
            "action": {
                "type": "expire"
            }
        }
    ]
}
EOF
}

data "aws_caller_identity" "current" {}

data "aws_iam_policy_document" "ecr_repository_policy_document" {
  statement {
    sid    = "AllowECRRepositoryAccess"
    effect = "Allow"

    principals {
      type        = "AWS"
      identifiers = [data.aws_caller_identity.current.account_id]
    }

    actions = [
      "ecr:GetDownloadUrlForLayer",
      "ecr:BatchGetImage",
      "ecr:BatchCheckLayerAvailability",
      "ecr:PutImage",
      "ecr:InitiateLayerUpload",
      "ecr:UploadLayerPart",
      "ecr:CompleteLayerUpload",
      "ecr:DescribeRepositories",
      "ecr:GetRepositoryPolicy",
      "ecr:ListImages",
      "ecr:DeleteRepository",
      "ecr:BatchDeleteImage",
      "ecr:SetRepositoryPolicy",
      "ecr:DeleteRepositoryPolicy",
    ]
  }
}

resource "aws_ecr_repository_policy" "ecr_repository_policy" {
  repository = aws_ecr_repository.ecr_repository.name
  policy     = data.aws_iam_policy_document.ecr_repository_policy_document.json
}

locals {
  aws_cli_command = var.application_environment == "aws" ? "aws" : "awslocal"
}

resource "null_resource" "login_to_ecr" {
  provisioner "local-exec" {
    command = "${local.aws_cli_command} ecr get-login-password --region ${data.aws_region.current.name} | docker login --username AWS --password-stdin ${aws_ecr_repository.ecr_repository.repository_url}"
  }
}
