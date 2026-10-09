mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "123456789012"
    }
  }
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }
  mock_resource "aws_lambda_function_url" {
    defaults = {
      function_url = "https://abc.lambda-url.us-east-1.on.aws/"
    }
  }
  mock_resource "aws_cloudfront_distribution" {
    defaults = {
      domain_name = "d111111abcdef8.cloudfront.net"
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

run "defaults_plan" {
  command = plan

  assert {
    condition     = one(aws_s3_bucket_notification.uploads.queue).filter_prefix == "courses/"
    error_message = "only course sources may notify the worker; chat attachments must not"
  }

  assert {
    condition     = aws_dynamodb_table.main.ttl[0].attribute_name == "expires_at" && aws_dynamodb_table.main.ttl[0].enabled
    error_message = "guest records expire through TTL on expires_at"
  }

  assert {
    condition     = aws_s3vectors_index.embed.index_name == "amazon.titan-embed-text-v2-0" && aws_s3vectors_index.embed.dimension == 1024
    error_message = "vector index must match vector.IndexName and the Titan V2 width"
  }

  assert {
    condition     = aws_acm_certificate.site[0].domain_name == "scholia-aws.mol.la" && aws_acm_certificate.site[0].validation_method == "DNS"
    error_message = "the app is served at scholia-aws.mol.la with a DNS-validated certificate"
  }

  assert {
    condition     = contains(one(aws_s3_bucket_cors_configuration.uploads.cors_rule).allowed_origins, "https://scholia-aws.mol.la")
    error_message = "browser uploads from the custom domain must pass CORS"
  }

  assert {
    condition     = local.api_environment.SCHOLIA_EMBED_MODEL == "amazon.titan-embed-text-v2:0" && local.worker_environment.SCHOLIA_EMBED_MODEL == local.api_environment.SCHOLIA_EMBED_MODEL
    error_message = "API and worker must share the project-wide embedding model"
  }

  assert {
    condition     = one([for f in one(aws_bedrock_guardrail.main.content_policy_config).filters_config : f if f.type == "PROMPT_ATTACK"]).output_strength == "NONE"
    error_message = "prompt attack filtering applies to input only"
  }

  assert {
    condition     = one([for f in one(aws_bedrock_guardrail.main.content_policy_config).filters_config : f.input_strength == "NONE" && f.output_strength == "HIGH" if f.type == "MISCONDUCT"])
    error_message = "questions about attacks reach the model; exploit steps in replies are still blocked"
  }

  assert {
    condition     = one([for f in one(aws_bedrock_guardrail.main.content_policy_config).filters_config : f if f.type == "PROMPT_ATTACK"]).input_strength == "LOW"
    error_message = "only high-confidence prompt attacks are blocked; exam instructions score low or medium"
  }

  assert {
    condition     = one(aws_lambda_event_source_mapping.uploads.scaling_config).maximum_concurrency == 2 && aws_sqs_queue.uploads.visibility_timeout_seconds == 3600
    error_message = "ingest takes at most two concurrent workers and the queue hides a message for six worker timeouts"
  }

  assert {
    condition     = alltrue([for r in aws_s3_bucket_lifecycle_configuration.uploads.rule : length(r.abort_incomplete_multipart_upload) == 0 if startswith(r.id, "expire-guest-")])
    error_message = "S3 rejects a multipart abort in a rule filtered by tags"
  }
}

run "secrets_are_passed_by_reference" {
  command = plan

  assert {
    condition = alltrue([
      for k in keys(local.api_environment) : !contains(["SCHOLIA_SESSION_SECRET", "TAVILY_API_KEY"], k)
    ])
    error_message = "secret values must never be set as environment variables"
  }
}

run "caps_are_configurable" {
  command = plan

  variables {
    daily_caps = {
      user_messages  = 1
      user_uploads   = 2
      user_web       = 3
      guest_messages = 4
      guest_uploads  = 5
      guest_web      = 6
    }
  }

  assert {
    condition     = local.api_environment.SCHOLIA_USER_DAILY_MESSAGES == "1" && local.api_environment.SCHOLIA_GUEST_DAILY_WEB == "6"
    error_message = "daily caps reach the API environment"
  }
}

run "rejects_non_profile_models" {
  command = plan

  variables {
    server_models = ["amazon.nova-2-lite-v1:0"]
  }

  expect_failures = [var.server_models]
}

run "cloudfront_domain_only" {
  command = plan

  variables {
    domain_name = ""
  }

  assert {
    condition     = length(aws_acm_certificate.site) == 0 && length(aws_acm_certificate_validation.site) == 0
    error_message = "an empty domain serves only the CloudFront host"
  }
}

run "rejects_bad_domain" {
  command = plan

  variables {
    domain_name = "Not A Host"
  }

  expect_failures = [var.domain_name]
}
