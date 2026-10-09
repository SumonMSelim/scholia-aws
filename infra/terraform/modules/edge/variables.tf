variable "name" {
  description = "Name prefix for edge resources."
  type        = string
}

variable "api_function_url" {
  description = "IAM-authenticated Lambda function URL serving /api/*."
  type        = string

  validation {
    condition     = startswith(var.api_function_url, "https://")
    error_message = "api_function_url must be an https URL."
  }
}

variable "api_function_name" {
  description = "Name of the API function, used to grant CloudFront invoke access."
  type        = string
}

variable "aliases" {
  description = "Custom host names served by the distribution. Each needs the certificate to cover it."
  type        = list(string)
  default     = []
}

variable "certificate_arn" {
  description = "Validated ACM certificate in us-east-1 for aliases. Null uses the default CloudFront certificate."
  type        = string
  default     = null
}

variable "price_class" {
  description = "CloudFront price class; PriceClass_100 is the cheapest."
  type        = string
  default     = "PriceClass_100"
}

variable "content_security_policy" {
  description = "Content-Security-Policy header value. Null builds the default policy around upload_origin."
  type        = string
  default     = null
}

variable "upload_origin" {
  description = "Origin the browser PUTs presigned uploads to and plays media from, such as https://bucket.s3.us-east-1.amazonaws.com. It is the only non-self origin in the default CSP."
  type        = string
  default     = "https://*.amazonaws.com"

  validation {
    condition     = can(regex("^https://[A-Za-z0-9*.-]+$", var.upload_origin))
    error_message = "upload_origin must be an https origin with no path."
  }
}
