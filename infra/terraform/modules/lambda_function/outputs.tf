output "function_name" {
  description = "Lambda function name."
  value       = aws_lambda_function.this.function_name
}

output "function_arn" {
  description = "Lambda function ARN."
  value       = aws_lambda_function.this.arn
}

output "function_url" {
  description = "Function URL, or null when disabled."
  value       = var.function_url ? aws_lambda_function_url.this[0].function_url : null
}

output "role_name" {
  description = "Execution role name."
  value       = aws_iam_role.this.name
}
