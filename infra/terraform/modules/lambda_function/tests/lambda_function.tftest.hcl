mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }
  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "123456789012"
    }
  }
  mock_data "aws_region" {
    defaults = {
      region = "us-east-1"
    }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = {
      arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/scholia-test-api"
    }
  }
  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::123456789012:role/scholia-test-api-role"
    }
  }
}

mock_provider "archive" {
  mock_data "archive_file" {
    defaults = {
      output_path         = "/tmp/fn.zip"
      output_base64sha256 = "c2hh"
    }
  }
}

variables {
  name        = "scholia-test-api"
  source_dir  = "/tmp/does-not-matter"
  kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/test"
}

run "defaults_to_arm64_al2023_without_url" {
  command = plan

  assert {
    condition     = aws_lambda_function.this.runtime == "provided.al2023"
    error_message = "runtime must be provided.al2023"
  }
  assert {
    condition     = aws_lambda_function.this.architectures == tolist(["arm64"])
    error_message = "architecture must be arm64"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.retention_in_days == 14
    error_message = "logs must be retained for 14 days"
  }
  assert {
    condition     = length(aws_lambda_function_url.this) == 0
    error_message = "no function URL unless requested"
  }
  assert {
    condition     = length(aws_iam_role_policy.extra) == 0
    error_message = "no extra policy unless provided"
  }
}

run "function_url_is_iam_authenticated_and_streaming" {
  command = plan

  variables {
    function_url       = true
    attach_policy_json = true
    policy_json        = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
  }

  assert {
    condition     = aws_lambda_function_url.this[0].authorization_type == "AWS_IAM"
    error_message = "function URL must require IAM auth"
  }
  assert {
    condition     = aws_lambda_function_url.this[0].invoke_mode == "RESPONSE_STREAM"
    error_message = "function URL must stream responses"
  }
  assert {
    condition     = length(aws_iam_role_policy.extra) == 1
    error_message = "extra policy must be attached when provided"
  }
}

run "rejects_invalid_name" {
  command = plan

  variables {
    name = "Bad_Name"
  }

  expect_failures = [var.name]
}

run "rejects_timeout_over_limit" {
  command = plan

  variables {
    timeout_seconds = 901
  }

  expect_failures = [var.timeout_seconds]
}

run "environment_decrypt_is_scoped_to_this_function" {
  command = plan

  assert {
    condition = tolist(one([
      for s in data.aws_iam_policy_document.base.statement : one(s.condition).values
      if s.sid == "DecryptEnvironment"
    ])) == tolist(["arn:aws:lambda:us-east-1:123456789012:function:scholia-test-api"])
    error_message = "kms:Decrypt must be limited to this function's environment encryption context"
  }
}
