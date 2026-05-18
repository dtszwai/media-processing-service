output "prompt_key_id" {
  description = "Key id (UUID) of the prompt envelope key. Wired through KMS_PROMPT_KEY_ID to backend bootstrap."
  value       = aws_kms_key.prompt.key_id
}

output "prompt_key_arn" {
  description = "ARN of the prompt envelope key — used by IAM policies that need kms:Encrypt/Decrypt."
  value       = aws_kms_key.prompt.arn
}

output "prompt_key_alias" {
  description = "Alias name (alias/<prefix>-prompts) of the prompt envelope key."
  value       = aws_kms_alias.prompt.name
}
