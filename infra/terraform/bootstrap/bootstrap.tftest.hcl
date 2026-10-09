mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "123456789012"
    }
  }
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{}"
    }
  }
}

variables {
  budget_email = "owner@example.com"
}

run "state_bucket_is_named_per_account_and_region" {
  command = plan

  assert {
    condition     = aws_s3_bucket.state.bucket == "scholia-tfstate-123456789012-us-east-1"
    error_message = "state bucket name must include account and region"
  }
}

run "state_bucket_is_versioned_encrypted_and_private" {
  command = plan

  assert {
    condition     = aws_s3_bucket_versioning.state.versioning_configuration[0].status == "Enabled"
    error_message = "state bucket must be versioned"
  }
  assert {
    condition     = one(aws_s3_bucket_server_side_encryption_configuration.state.rule).apply_server_side_encryption_by_default[0].sse_algorithm == "AES256"
    error_message = "state bucket must be encrypted"
  }
  assert {
    condition = alltrue([
      aws_s3_bucket_public_access_block.state.block_public_acls,
      aws_s3_bucket_public_access_block.state.block_public_policy,
      aws_s3_bucket_public_access_block.state.ignore_public_acls,
      aws_s3_bucket_public_access_block.state.restrict_public_buckets,
    ])
    error_message = "state bucket must block all public access"
  }
}

run "budget_alerts_owner" {
  command = plan

  assert {
    condition     = aws_budgets_budget.monthly.limit_amount == "20"
    error_message = "default budget must be 20 USD"
  }
  assert {
    condition     = alltrue([for n in aws_budgets_budget.monthly.notification : contains(n.subscriber_email_addresses, "owner@example.com")])
    error_message = "every budget notification must reach the owner"
  }
}

run "rejects_invalid_email" {
  command = plan

  variables {
    budget_email = "not-an-email"
  }

  expect_failures = [var.budget_email]
}

run "account_guardrails_are_on" {
  command = plan

  assert {
    condition = (
      aws_s3_account_public_access_block.this.block_public_acls &&
      aws_s3_account_public_access_block.this.ignore_public_acls &&
      aws_s3_account_public_access_block.this.block_public_policy &&
      aws_s3_account_public_access_block.this.restrict_public_buckets
    )
    error_message = "the account must block public S3 access by default"
  }
}
