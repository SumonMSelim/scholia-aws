variable "name" {
  description = "Function name; also used for the role and log group."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]{3,56}$", var.name))
    error_message = "name must be 3-56 lowercase letters, digits or hyphens."
  }
}

variable "source_dir" {
  description = "Directory containing the compiled bootstrap binary."
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key for environment variables and log group encryption."
  type        = string
}

variable "memory_mb" {
  description = "Memory in MB; CPU scales with it."
  type        = number
  default     = 512
}

variable "timeout_seconds" {
  description = "Maximum run time per invocation."
  type        = number
  default     = 60

  validation {
    condition     = var.timeout_seconds >= 1 && var.timeout_seconds <= 900
    error_message = "timeout_seconds must be between 1 and 900."
  }
}

variable "reserved_concurrency" {
  description = "Reserved concurrency; null leaves the function on the shared pool."
  type        = number
  default     = null
}

variable "environment" {
  description = "Environment variables for the function."
  type        = map(string)
  default     = {}
}

variable "policy_json" {
  description = "Additional IAM policy for the function's role; used when attach_policy_json is true."
  type        = string
  default     = null
}

variable "attach_policy_json" {
  description = "Attach policy_json to the role. A separate flag because the policy is often unknown until apply."
  type        = bool
  default     = false
}

variable "log_retention_days" {
  description = "CloudWatch log retention."
  type        = number
  default     = 14
}

variable "function_url" {
  description = "Create an IAM-authenticated function URL."
  type        = bool
  default     = false
}

variable "stream_response" {
  description = "Use response streaming on the function URL."
  type        = bool
  default     = true
}
