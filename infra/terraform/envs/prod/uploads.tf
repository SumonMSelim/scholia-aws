# Course files. The browser PUTs with a presigned URL. S3 notifies the queue, and the worker
# checks size and magic bytes. The queue DLQ is the failure path. A new account's
# concurrency limit is 10, so reserved concurrency stays unset until a quota increase.

resource "aws_s3_bucket" "uploads" {
  #checkov:skip=CKV_AWS_144:Course files are rebuilt by re-upload. Cross-region replication adds cost.
  #checkov:skip=CKV_AWS_18:Access logging for uploads would store a second copy of every object name at extra cost.
  bucket = "${local.name}-uploads-${local.account_id}"
}

resource "aws_s3_bucket_ownership_controls" "uploads" {
  bucket = aws_s3_bucket.uploads.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_public_access_block" "uploads" {
  bucket                  = aws_s3_bucket.uploads.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# The browser PUTs a presigned URL from the site origin. The signature authorizes the write.
resource "aws_s3_bucket_cors_configuration" "uploads" {
  bucket = aws_s3_bucket.uploads.id

  cors_rule {
    allowed_headers = ["*"]
    allowed_methods = ["PUT", "GET", "HEAD"]
    allowed_origins = concat(["https://${module.edge.domain_name}"], local.custom_domain ? ["https://${var.domain_name}"] : [])
    expose_headers  = ["ETag"]
    max_age_seconds = 3000
  }
}

resource "aws_s3_bucket_versioning" "uploads" {
  bucket = aws_s3_bucket.uploads.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "uploads" {
  bucket = aws_s3_bucket.uploads.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.main.arn
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "uploads" {
  #checkov:skip=CKV_AWS_300:The first rule aborts every incomplete upload; S3 rejects an abort in the tag-filtered guest rules.
  bucket = aws_s3_bucket.uploads.id
  rule {
    id     = "abort-incomplete-and-expire-old-versions"
    status = "Enabled"
    filter {}
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }

  # Guest files are tagged guest=true at upload and outlive their DynamoDB rows by a day.
  dynamic "rule" {
    for_each = ["courses/", "chats/"]
    content {
      id     = "expire-guest-${trimsuffix(rule.value, "/")}"
      status = "Enabled"
      filter {
        and {
          prefix = rule.value
          tags   = { guest = "true" }
        }
      }
      expiration {
        days = 3
      }
      noncurrent_version_expiration {
        noncurrent_days = 1
      }
      # S3 refuses a multipart abort on a tag filter; the first rule covers it.
    }
  }
}

data "aws_iam_policy_document" "uploads_bucket" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.uploads.arn, "${aws_s3_bucket.uploads.arn}/*"]
    principals {
      type        = "*"
      identifiers = ["*"]
    }
    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "uploads" {
  bucket = aws_s3_bucket.uploads.id
  policy = data.aws_iam_policy_document.uploads_bucket.json
}

resource "aws_sqs_queue" "uploads_dlq" {
  name                    = "${local.name}-uploads-dlq"
  sqs_managed_sse_enabled = true
}

resource "aws_sqs_queue" "uploads" {
  name                    = "${local.name}-uploads"
  sqs_managed_sse_enabled = true
  # Six times the worker timeout, as AWS advises for a Lambda consumer.
  visibility_timeout_seconds = 3600
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.uploads_dlq.arn
    maxReceiveCount     = 5
  })
}

data "aws_iam_policy_document" "uploads_queue" {
  statement {
    sid       = "AllowS3"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.uploads.arn]
    principals {
      type        = "Service"
      identifiers = ["s3.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_s3_bucket.uploads.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

resource "aws_sqs_queue_policy" "uploads" {
  queue_url = aws_sqs_queue.uploads.id
  policy    = data.aws_iam_policy_document.uploads_queue.json
}

# Only course sources notify the worker. Chat attachments live under chats/.
resource "aws_s3_bucket_notification" "uploads" {
  bucket = aws_s3_bucket.uploads.id

  queue {
    queue_arn     = aws_sqs_queue.uploads.arn
    events        = ["s3:ObjectCreated:*"]
    filter_prefix = "courses/"
  }

  depends_on = [aws_sqs_queue_policy.uploads]
}

data "aws_iam_policy_document" "worker" {
  statement {
    sid = "TableAccess"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
    ]
    resources = [aws_dynamodb_table.main.arn]
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
    sid       = "ReadUploads"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.uploads.arn}/*"]
  }

  statement {
    sid = "WriteDerived"
    # Objects derived from a guest source carry guest=true so the lifecycle rule removes them.
    actions   = ["s3:PutObject", "s3:PutObjectTagging"]
    resources = ["${aws_s3_bucket.uploads.arn}/*"]
  }

  statement {
    sid       = "EmbedChunks"
    actions   = ["bedrock:InvokeModel"]
    resources = ["arn:aws:bedrock:${var.region}::foundation-model/${var.embed_model}"]
  }

  # The index is created by Terraform, so the worker only writes to it.
  statement {
    sid       = "WriteVectors"
    actions   = ["s3vectors:PutVectors", "s3vectors:GetIndex"]
    resources = [aws_s3vectors_index.embed.index_arn]
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

  statement {
    sid = "ConsumeQueue"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility",
    ]
    resources = [aws_sqs_queue.uploads.arn]
  }
}

module "worker" {
  source = "../../modules/lambda_function"

  name        = "${local.name}-worker"
  source_dir  = var.worker_artifact_dir
  kms_key_arn = aws_kms_key.main.arn
  memory_mb   = 512
  # A textbook PDF embeds hundreds of chunks one call at a time. A timeout
  # mid-file would retry it and store every chunk again.
  timeout_seconds      = 600
  function_url         = false
  attach_policy_json   = true
  policy_json          = data.aws_iam_policy_document.worker.json
  reserved_concurrency = var.worker_reserved_concurrency

  environment = local.worker_environment
}

locals {
  worker_environment = {
    SCHOLIA_ENV           = "prod"
    LOG_LEVEL             = "info"
    TABLE_NAME            = aws_dynamodb_table.main.name
    UPLOADS_BUCKET        = aws_s3_bucket.uploads.bucket
    SCHOLIA_EMBED_MODEL   = var.embed_model
    SCHOLIA_VECTOR_BUCKET = aws_s3vectors_vector_bucket.main.vector_bucket_name
  }
}

resource "aws_lambda_event_source_mapping" "uploads" {
  event_source_arn = aws_sqs_queue.uploads.arn
  function_name    = module.worker.function_arn
  batch_size       = 1

  # Ingest shares the account's concurrency with the API; a burst of uploads
  # must not take the slots judges' chats need.
  scaling_config {
    maximum_concurrency = 2
  }
}
