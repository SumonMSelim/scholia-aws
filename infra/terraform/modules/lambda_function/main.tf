terraform {
  required_version = ">= 1.16.0, < 2.0.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.66"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.8"
    }
  }
}

data "aws_caller_identity" "current" {}

data "aws_region" "current" {}

data "archive_file" "code" {
  type        = "zip"
  source_dir  = var.source_dir
  output_path = "${path.root}/.build/${var.name}.zip"
}

resource "aws_cloudwatch_log_group" "this" {
  #checkov:skip=CKV_AWS_338:14-day retention is a deliberate cost decision for application logs.
  name              = "/aws/lambda/${var.name}"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn
}

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

data "aws_iam_policy_document" "base" {
  statement {
    sid       = "WriteLogs"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.this.arn}:*"]
  }

  # Only this function's own environment. The key is shared, and an unconditioned
  # Decrypt would let any function open every ciphertext under it, such as sealed
  # provider keys read from the table.
  statement {
    sid       = "DecryptEnvironment"
    actions   = ["kms:Decrypt"]
    resources = [var.kms_key_arn]
    condition {
      test     = "StringEquals"
      variable = "kms:EncryptionContext:aws:lambda:FunctionArn"
      values   = ["arn:aws:lambda:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:function:${var.name}"]
    }
  }
}

resource "aws_iam_role_policy" "base" {
  name   = "base"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.base.json
}

resource "aws_iam_role_policy" "extra" {
  count  = var.attach_policy_json ? 1 : 0
  name   = "app"
  role   = aws_iam_role.this.id
  policy = var.policy_json
}

resource "aws_lambda_function" "this" {
  #checkov:skip=CKV_AWS_117:No VPC by design; a NAT gateway would cost more than the whole app.
  #checkov:skip=CKV_AWS_116:Synchronous function URL handler; failures are returned to the caller.
  #checkov:skip=CKV_AWS_115:Account concurrency limit is 10, which leaves no room for reserved concurrency.
  #checkov:skip=CKV_AWS_272:Code signing is out of scope; artifacts are built and deployed by CI only.
  #checkov:skip=CKV_AWS_50:X-Ray tracing disabled to keep cost at zero; structured logs are used instead.
  function_name    = var.name
  role             = aws_iam_role.this.arn
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  handler          = "bootstrap"
  filename         = data.archive_file.code.output_path
  source_code_hash = data.archive_file.code.output_base64sha256
  memory_size      = var.memory_mb
  timeout          = var.timeout_seconds
  kms_key_arn      = var.kms_key_arn

  reserved_concurrent_executions = var.reserved_concurrency

  environment {
    variables = var.environment
  }

  logging_config {
    log_format = "JSON"
    log_group  = aws_cloudwatch_log_group.this.name
  }

  depends_on = [aws_iam_role_policy.base]
}

resource "aws_lambda_function_url" "this" {
  count              = var.function_url ? 1 : 0
  function_name      = aws_lambda_function.this.function_name
  authorization_type = "AWS_IAM"
  invoke_mode        = var.stream_response ? "RESPONSE_STREAM" : "BUFFERED"
}
