variable "region" {
  description = "AWS region for all resources."
  type        = string
  default     = "us-east-1"
}

variable "domain_name" {
  description = "Public host name for the app, served by CloudFront. Empty serves only the CloudFront domain."
  type        = string
  default     = "scholia-aws.mol.la"

  validation {
    condition     = var.domain_name == "" || can(regex("^([a-z0-9]([a-z0-9-]*[a-z0-9])?\\.)+[a-z]{2,}$", var.domain_name))
    error_message = "domain_name must be a lower-case host name, or empty."
  }
}

variable "api_artifact_dir" {
  description = "Directory holding the compiled API bootstrap binary (make build)."
  type        = string
  default     = "../../../../dist/api"
}

variable "worker_artifact_dir" {
  description = "Directory holding the compiled worker bootstrap binary (make build)."
  type        = string
  default     = "../../../../dist/worker"
}

variable "default_model" {
  description = "Server-paid chat model. Must be a Bedrock id in internal/model/catalog.go."
  type        = string
  default     = "us.amazon.nova-2-lite-v1:0"
}

variable "server_models" {
  description = "Bedrock inference profiles the API may call on the server's account."
  type        = list(string)
  default     = ["us.amazon.nova-2-lite-v1:0"]

  validation {
    condition     = alltrue([for m in var.server_models : startswith(m, "us.")])
    error_message = "server_models must be US inference profile ids (us.<provider>.<model>)."
  }
}

variable "bedrock_regions" {
  description = "Regions a US inference profile may route a request to. The role must allow the foundation model in each."
  type        = list(string)
  default     = ["us-east-1", "us-east-2", "us-west-2"]
}

variable "embed_model" {
  description = "Bedrock embedding model (SCHOLIA_EMBED_MODEL). The vector index is named after it."
  type        = string
  default     = "amazon.titan-embed-text-v2:0"
}

variable "embed_dimension" {
  description = "Vector width the embedding model returns. Titan Text Embeddings V2 defaults to 1024."
  type        = number
  default     = 1024
}

variable "max_output_tokens" {
  description = "Cap on model output tokens per call, to bound cost."
  type        = number
  default     = 2048

  validation {
    condition     = var.max_output_tokens >= 1 && var.max_output_tokens <= 32000
    error_message = "max_output_tokens must be between 1 and 32000."
  }
}

variable "guests" {
  description = "Allow one-click guest sessions. The kill switch can also turn them off at run time."
  type        = bool
  default     = true
}

variable "daily_caps" {
  description = "Per-account daily quotas for signed-in users and guests."
  type = object({
    user_messages  = number
    user_uploads   = number
    user_web       = number
    guest_messages = number
    guest_uploads  = number
    guest_web      = number
  })
  default = {
    user_messages  = 100
    user_uploads   = 30
    user_web       = 40
    guest_messages = 30
    guest_uploads  = 5
    guest_web      = 10
  }

  validation {
    condition     = alltrue([for v in values(var.daily_caps) : v >= 0])
    error_message = "daily_caps values must be zero or positive."
  }
}

variable "guest_sessions_per_hour" {
  description = "Guest sessions one address may start per hour, per API instance."
  type        = number
  default     = 5

  validation {
    condition     = var.guest_sessions_per_hour >= 1 && var.guest_sessions_per_hour <= 100
    error_message = "guest_sessions_per_hour must be between 1 and 100."
  }
}

variable "guest_ttl_hours" {
  description = "How long guest data lives before DynamoDB TTL removes it."
  type        = number
  default     = 48

  validation {
    condition     = var.guest_ttl_hours >= 1 && var.guest_ttl_hours <= 168
    error_message = "guest_ttl_hours must be between 1 and 168."
  }
}

variable "api_reserved_concurrency" {
  description = "Reserved concurrency for the API. Null uses the shared pool: a new account's limit is 10 and AWS keeps 10 unreserved, so nothing can be reserved until a quota increase."
  type        = number
  default     = null
}

variable "worker_reserved_concurrency" {
  description = "Reserved concurrency for the worker. Null uses the shared pool; see api_reserved_concurrency."
  type        = number
  default     = null
}

variable "ses_from_address" {
  description = "Verified SES email identity Cognito sends sign-in codes from. Null uses the Cognito default sender."
  type        = string
  default     = null
}
