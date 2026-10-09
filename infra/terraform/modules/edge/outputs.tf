output "domain_name" {
  description = "CloudFront domain name."
  value       = aws_cloudfront_distribution.this.domain_name
}

output "distribution_id" {
  description = "CloudFront distribution ID, used for cache invalidation."
  value       = aws_cloudfront_distribution.this.id
}

output "site_bucket" {
  description = "Bucket holding the built web app."
  value       = aws_s3_bucket.site.bucket
}
