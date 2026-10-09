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
    # Ephemeral random_password generates the session secret without writing it to state.
    random = {
      source  = "hashicorp/random"
      version = "~> 3.7"
    }
  }

  backend "s3" {
    key          = "prod/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = "scholia"
      Environment = "prod"
      ManagedBy   = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}

locals {
  name       = "scholia-prod"
  account_id = data.aws_caller_identity.current.account_id
}

# --- Encryption -----------------------------------------------------------------

data "aws_iam_policy_document" "kms" {
  #checkov:skip=CKV_AWS_111:Key policy; "*" refers to this key only.
  #checkov:skip=CKV_AWS_356:Key policy; "*" refers to this key only.
  #checkov:skip=CKV_AWS_109:Key policy; administration is limited to the account root principal.
  statement {
    sid       = "AccountAdministration"
    actions   = ["kms:*"]
    resources = ["*"]
    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${local.account_id}:root"]
    }
  }

  statement {
    sid       = "CloudWatchLogs"
    actions   = ["kms:Encrypt*", "kms:Decrypt*", "kms:ReEncrypt*", "kms:GenerateDataKey*", "kms:Describe*"]
    resources = ["*"]
    principals {
      type        = "Service"
      identifiers = ["logs.${var.region}.amazonaws.com"]
    }
    condition {
      test     = "ArnLike"
      variable = "kms:EncryptionContext:aws:logs:arn"
      values   = ["arn:aws:logs:${var.region}:${local.account_id}:log-group:*"]
    }
  }
}

resource "aws_kms_key" "main" {
  description             = "Scholia prod: data, logs and user provider keys"
  enable_key_rotation     = true
  deletion_window_in_days = 30
  policy                  = data.aws_iam_policy_document.kms.json
}

resource "aws_kms_alias" "main" {
  name          = "alias/${local.name}"
  target_key_id = aws_kms_key.main.key_id
}

# --- Data -------------------------------------------------------------------------

resource "aws_dynamodb_table" "main" {
  name                        = local.name
  billing_mode                = "PAY_PER_REQUEST"
  hash_key                    = "pk"
  range_key                   = "sk"
  deletion_protection_enabled = true

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  # Guest records and usage counters carry expires_at (epoch seconds).
  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  server_side_encryption {
    enabled     = true
    kms_key_arn = aws_kms_key.main.arn
  }
}

# --- API ------------------------------------------------------------------------

data "aws_iam_policy_document" "api" {
  statement {
    sid = "TableAccess"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
      "dynamodb:Query",
      "dynamodb:BatchGetItem",
      "dynamodb:BatchWriteItem",
      "dynamodb:ConditionCheckItem",
    ]
    resources = [aws_dynamodb_table.main.arn, "${aws_dynamodb_table.main.arn}/index/*"]
  }

  statement {
    sid       = "TableEncryption"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [aws_kms_key.main.arn]
    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["dynamodb.${var.region}.amazonaws.com"]
    }
  }

  statement {
    sid = "SignUploads"
    # Guest uploads are presigned with x-amz-tagging, so the signer needs tagging too.
    actions   = ["s3:PutObject", "s3:PutObjectTagging"]
    resources = ["${aws_s3_bucket.uploads.arn}/*"]
  }

  statement {
    sid       = "UploadEncryption"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [aws_kms_key.main.arn]
    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["s3.${var.region}.amazonaws.com"]
    }
  }

  # Provider keys are sealed in this function, which is also the function that calls the model.
  # The user id is the encryption context, so a ciphertext cannot be opened for someone else.
  statement {
    sid       = "ProviderKey"
    actions   = ["kms:Encrypt", "kms:Decrypt"]
    resources = [aws_kms_key.main.arn]
    condition {
      test     = "Null"
      variable = "kms:EncryptionContext:user_id"
      values   = ["false"]
    }
  }

  # Chat attachments are read once at send time and removed with their chat.
  statement {
    sid       = "ChatAttachments"
    actions   = ["s3:GetObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.uploads.arn}/chats/*"]
  }

  # Server-paid chat models, through US inference profiles, and the embedding model.
  statement {
    sid       = "InvokeServerModels"
    actions   = ["bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"]
    resources = local.bedrock_invoke_resources
  }

  statement {
    sid       = "ApplyGuardrail"
    actions   = ["bedrock:ApplyGuardrail"]
    resources = [aws_bedrock_guardrail.main.guardrail_arn]
  }

  statement {
    sid       = "QueryVectors"
    actions   = ["s3vectors:QueryVectors", "s3vectors:GetVectors", "s3vectors:GetIndex"]
    resources = [aws_s3vectors_index.embed.index_arn]
  }

  statement {
    sid     = "ReadSecrets"
    actions = ["secretsmanager:GetSecretValue"]
    resources = [
      aws_secretsmanager_secret.session.arn,
      aws_secretsmanager_secret.tavily.arn,
    ]
  }

  statement {
    sid       = "SecretEncryption"
    actions   = ["kms:Decrypt"]
    resources = [aws_kms_key.main.arn]
    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["secretsmanager.${var.region}.amazonaws.com"]
    }
  }

  statement {
    sid       = "ReadKillSwitch"
    actions   = ["ssm:GetParameter"]
    resources = [aws_ssm_parameter.kill_switch.arn]
  }
}

# Secrets are passed by ARN and read at cold start, never as values.
locals {
  api_environment = {
    SCHOLIA_ENV                     = "prod"
    LOG_LEVEL                       = "info"
    TABLE_NAME                      = aws_dynamodb_table.main.name
    UPLOADS_BUCKET                  = aws_s3_bucket.uploads.bucket
    SCHOLIA_KMS_KEY_ID              = aws_kms_alias.main.name
    SCHOLIA_VECTOR_BUCKET           = aws_s3vectors_vector_bucket.main.vector_bucket_name
    SCHOLIA_COGNITO_CLIENT_ID       = aws_cognito_user_pool_client.web.id
    SCHOLIA_SESSION_SECRET_ARN      = aws_secretsmanager_secret.session.arn
    SCHOLIA_TAVILY_SECRET_ARN       = aws_secretsmanager_secret.tavily.arn
    SCHOLIA_BEDROCK                 = "on"
    SCHOLIA_DEFAULT_MODEL           = var.default_model
    SCHOLIA_EMBED_MODEL             = var.embed_model
    SCHOLIA_MAX_OUTPUT_TOKENS       = tostring(var.max_output_tokens)
    SCHOLIA_GUARDRAIL_ID            = aws_bedrock_guardrail.main.guardrail_id
    SCHOLIA_GUARDRAIL_VERSION       = aws_bedrock_guardrail_version.main.version
    SCHOLIA_KILL_SWITCH_PARAM       = aws_ssm_parameter.kill_switch.name
    SCHOLIA_GUESTS                  = var.guests ? "on" : "off"
    SCHOLIA_GUEST_TTL_HOURS         = tostring(var.guest_ttl_hours)
    SCHOLIA_GUEST_SESSIONS_PER_HOUR = tostring(var.guest_sessions_per_hour)
    SCHOLIA_USER_DAILY_MESSAGES     = tostring(var.daily_caps.user_messages)
    SCHOLIA_USER_DAILY_UPLOADS      = tostring(var.daily_caps.user_uploads)
    SCHOLIA_USER_DAILY_WEB          = tostring(var.daily_caps.user_web)
    SCHOLIA_GUEST_DAILY_MESSAGES    = tostring(var.daily_caps.guest_messages)
    SCHOLIA_GUEST_DAILY_UPLOADS     = tostring(var.daily_caps.guest_uploads)
    SCHOLIA_GUEST_DAILY_WEB         = tostring(var.daily_caps.guest_web)
  }
}

module "api" {
  source = "../../modules/lambda_function"

  name                 = "${local.name}-api"
  source_dir           = var.api_artifact_dir
  kms_key_arn          = aws_kms_key.main.arn
  memory_mb            = 512
  timeout_seconds      = 60
  function_url         = true
  attach_policy_json   = true
  policy_json          = data.aws_iam_policy_document.api.json
  reserved_concurrency = var.api_reserved_concurrency

  environment = local.api_environment
}

# --- Edge -------------------------------------------------------------------------

module "edge" {
  source = "../../modules/edge"

  name              = local.name
  api_function_url  = module.api.function_url
  api_function_name = module.api.function_name
  aliases           = local.custom_domain ? [var.domain_name] : []
  certificate_arn   = local.custom_domain ? aws_acm_certificate_validation.site[0].certificate_arn : null
  upload_origin     = "https://${aws_s3_bucket.uploads.bucket_regional_domain_name}"
}
