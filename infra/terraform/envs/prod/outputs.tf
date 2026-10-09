output "url" {
  description = "Public URL of the application."
  value       = local.custom_domain ? "https://${var.domain_name}" : "https://${module.edge.domain_name}"
}

output "distribution_id" {
  description = "CloudFront distribution ID."
  value       = module.edge.distribution_id
}

output "site_bucket" {
  description = "Bucket for the built web app."
  value       = module.edge.site_bucket
}

output "api_function_name" {
  description = "API Lambda function name."
  value       = module.api.function_name
}

output "table_name" {
  description = "DynamoDB table name."
  value       = aws_dynamodb_table.main.name
}

output "uploads_bucket" {
  description = "Uploads bucket the worker ingests from; cmd/seed writes the demo course here."
  value       = aws_s3_bucket.uploads.bucket
}

output "api_function_url" {
  description = "IAM-authenticated function URL. Only CloudFront can call it; use the public url."
  value       = module.api.function_url
}

output "vector_bucket" {
  description = "S3 Vectors bucket holding course embeddings."
  value       = aws_s3vectors_vector_bucket.main.vector_bucket_name
}

output "cognito_user_pool_id" {
  description = "Cognito user pool for email sign-in."
  value       = aws_cognito_user_pool.main.id
}

output "cognito_client_id" {
  description = "Cognito app client the API signs users in with."
  value       = aws_cognito_user_pool_client.web.id
}

output "session_secret_arn" {
  description = "Secrets Manager secret that signs session tokens. Generated at apply; not in state."
  value       = aws_secretsmanager_secret.session.arn
}

output "tavily_secret_arn" {
  description = "Secrets Manager secret for the Tavily key. Put the value yourself after apply."
  value       = aws_secretsmanager_secret.tavily.arn
}

output "guardrail_id" {
  description = "Bedrock guardrail id."
  value       = aws_bedrock_guardrail.main.guardrail_id
}

output "guardrail_version" {
  description = "Published Bedrock guardrail version the API uses."
  value       = aws_bedrock_guardrail_version.main.version
}

output "kill_switch_parameter" {
  description = "SSM parameter holding the run-time kill switch."
  value       = aws_ssm_parameter.kill_switch.name
}

output "cloudflare_validation_record" {
  description = "Add in Cloudflare as a DNS-only CNAME so ACM can validate the certificate."
  value = local.custom_domain ? [for o in aws_acm_certificate.site[0].domain_validation_options : {
    type    = o.resource_record_type
    name    = o.resource_record_name
    content = o.resource_record_value
  }] : []
}

output "cloudflare_site_record" {
  description = "Add in Cloudflare as a DNS-only (grey cloud) CNAME. Proxying would put a second CDN in front of CloudFront."
  value = local.custom_domain ? {
    type    = "CNAME"
    name    = var.domain_name
    content = module.edge.domain_name
  } : null
}
