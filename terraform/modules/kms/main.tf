terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0, < 6.0"
    }
  }
}

# Customer-managed key for AES-256-GCM data-key wrapping of stored prompts.
# Backend code reads KMS_PROMPT_KEY_ID and uses it via internal/infra/sealer/kms.
#
# Terraform creates the LocalStack key for `make tf-up`; plain `make up`
# falls back to bootstrap.ensureLocalPromptKey when no key id is provided.
resource "aws_kms_key" "prompt" {
  description             = "${var.name_prefix} prompt envelope key (AES-256-GCM data-key wrapping)"
  enable_key_rotation     = true
  deletion_window_in_days = 30

  tags = merge(var.additional_tags, {
    Name = "${var.name_prefix}-prompt-key"
  })
}

resource "aws_kms_alias" "prompt" {
  name          = "alias/${var.name_prefix}-prompts"
  target_key_id = aws_kms_key.prompt.key_id
}
