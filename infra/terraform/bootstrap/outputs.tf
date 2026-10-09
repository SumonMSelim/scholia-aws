output "state_bucket" {
  description = "Name of the remote state bucket used by the other stacks."
  value       = aws_s3_bucket.state.bucket
}
