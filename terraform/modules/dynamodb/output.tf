output "dynamodb_table_name" {
  value = aws_dynamodb_table.media_dynamo_table.name
}

output "dynamodb_table_arn" {
  value = aws_dynamodb_table.media_dynamo_table.arn
}
