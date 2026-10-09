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
  mock_data "aws_cloudfront_cache_policy" {
    defaults = {
      id = "cache-policy-id"
    }
  }
  mock_data "aws_cloudfront_origin_request_policy" {
    defaults = {
      id = "origin-request-policy-id"
    }
  }
  mock_resource "aws_cloudfront_distribution" {
    defaults = {
      arn = "arn:aws:cloudfront::123456789012:distribution/EXAMPLE"
    }
  }
  mock_resource "aws_s3_bucket" {
    defaults = {
      arn                         = "arn:aws:s3:::scholia-test-site-123456789012"
      bucket_regional_domain_name = "scholia-test-site-123456789012.s3.us-east-1.amazonaws.com"
    }
  }
  mock_resource "aws_cloudfront_function" {
    defaults = {
      arn = "arn:aws:cloudfront::123456789012:function/scholia-test-spa-rewrite"
    }
  }
}

variables {
  name              = "scholia-test"
  api_function_url  = "https://abc123.lambda-url.us-east-1.on.aws/"
  api_function_name = "scholia-test-api"
}

run "site_bucket_is_private" {
  command = plan

  assert {
    condition     = aws_s3_bucket.site.bucket == "scholia-test-site-123456789012"
    error_message = "site bucket must be named per account"
  }
  assert {
    condition = alltrue([
      aws_s3_bucket_public_access_block.site.block_public_acls,
      aws_s3_bucket_public_access_block.site.block_public_policy,
      aws_s3_bucket_public_access_block.site.ignore_public_acls,
      aws_s3_bucket_public_access_block.site.restrict_public_buckets,
    ])
    error_message = "site bucket must block public access"
  }
}

run "api_origin_uses_function_url_host_with_oac" {
  command = plan

  assert {
    condition     = contains([for o in aws_cloudfront_distribution.this.origin : o.domain_name], "abc123.lambda-url.us-east-1.on.aws")
    error_message = "api origin must be the bare function URL host"
  }
  assert {
    condition     = aws_cloudfront_origin_access_control.api.origin_access_control_origin_type == "lambda"
    error_message = "api OAC must target lambda"
  }
  assert {
    condition     = aws_cloudfront_origin_access_control.site.origin_access_control_origin_type == "s3"
    error_message = "site OAC must target s3"
  }
}

run "api_behaviour_is_uncached_and_https_only" {
  command = plan

  assert {
    condition     = one(aws_cloudfront_distribution.this.ordered_cache_behavior).path_pattern == "/api/*"
    error_message = "api behaviour must match /api/*"
  }
  assert {
    condition     = one(aws_cloudfront_distribution.this.ordered_cache_behavior).viewer_protocol_policy == "https-only"
    error_message = "api must be https only"
  }
  assert {
    condition     = contains(one(aws_cloudfront_distribution.this.ordered_cache_behavior).allowed_methods, "POST")
    error_message = "api must accept POST"
  }
}

run "cloudfront_can_invoke_the_function_url" {
  command = plan

  assert {
    condition     = aws_lambda_permission.invoke_url.action == "lambda:InvokeFunctionUrl" && aws_lambda_permission.invoke_url.function_url_auth_type == "AWS_IAM"
    error_message = "CloudFront must be allowed to invoke the IAM function URL"
  }
  assert {
    condition     = aws_lambda_permission.invoke.action == "lambda:InvokeFunction"
    error_message = "CloudFront must also be allowed lambda:InvokeFunction"
  }
}

run "security_headers_are_enforced" {
  command = plan

  assert {
    condition     = one(one(aws_cloudfront_response_headers_policy.security.security_headers_config).frame_options).frame_option == "DENY"
    error_message = "frames must be denied"
  }
  assert {
    condition     = strcontains(one(one(aws_cloudfront_response_headers_policy.security.security_headers_config).content_security_policy).content_security_policy, "frame-ancestors 'none'")
    error_message = "CSP must forbid framing"
  }
  assert {
    condition     = strcontains(one(one(aws_cloudfront_response_headers_policy.security.security_headers_config).content_security_policy).content_security_policy, "frame-src https://www.youtube-nocookie.com;")
    error_message = "CSP may frame only the privacy-enhanced YouTube player"
  }
}

run "csp_pins_the_upload_origin" {
  command = plan

  variables {
    upload_origin = "https://scholia-test-uploads.s3.us-east-1.amazonaws.com"
  }

  assert {
    condition = (
      strcontains(local.content_security_policy, "connect-src 'self' https://scholia-test-uploads.s3.us-east-1.amazonaws.com;") &&
      !strcontains(local.content_security_policy, "*.amazonaws.com")
    )
    error_message = "the CSP must allow only the uploads bucket, not every amazonaws.com host"
  }
}

run "api_forwards_only_the_headers_it_reads" {
  command = plan

  assert {
    condition = (
      one(aws_cloudfront_origin_request_policy.api.headers_config).header_behavior == "whitelist" &&
      contains(one(one(aws_cloudfront_origin_request_policy.api.headers_config).headers).items, "CloudFront-Viewer-Address") &&
      contains(one(one(aws_cloudfront_origin_request_policy.api.headers_config).headers).items, "x-scholia-token") &&
      !contains(one(one(aws_cloudfront_origin_request_policy.api.headers_config).headers).items, "host") &&
      !contains(one(one(aws_cloudfront_origin_request_policy.api.headers_config).headers).items, "x-amz-content-sha256") &&
      !contains(one(one(aws_cloudfront_origin_request_policy.api.headers_config).headers).items, "x-forwarded-for")
    )
    error_message = "the API gets the session and viewer address headers, never Host or a client X-Forwarded-For"
  }
}

run "rejects_bad_upload_origin" {
  command = plan

  variables {
    upload_origin = "http://bucket.example.com/path"
  }

  expect_failures = [var.upload_origin]
}

run "rejects_non_https_function_url" {
  command = plan

  variables {
    api_function_url = "http://insecure.example.com/"
  }

  expect_failures = [var.api_function_url]
}

run "custom_domain_uses_the_certificate" {
  command = plan

  variables {
    aliases         = ["scholia.example.org"]
    certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/abc"
  }

  assert {
    condition     = one(aws_cloudfront_distribution.this.viewer_certificate).minimum_protocol_version == "TLSv1.2_2021" && one(aws_cloudfront_distribution.this.viewer_certificate).ssl_support_method == "sni-only"
    error_message = "a custom domain must use SNI and TLS 1.2 or later"
  }
}

run "aliases_need_a_certificate" {
  command = plan

  variables {
    aliases = ["scholia.example.org"]
  }

  expect_failures = [aws_cloudfront_distribution.this]
}
