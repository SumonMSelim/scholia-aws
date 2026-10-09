# Custom domain. DNS for the domain lives in Cloudflare, so the validation
# and the site CNAME are added there by hand. See the domain outputs.
#
# Apply in two steps on the first run: the certificate first, so its validation
# record can be added in Cloudflare, then everything else once it validates.
#   terraform apply -target=aws_acm_certificate.site
#   terraform apply

locals {
  custom_domain = var.domain_name != ""
}

resource "aws_acm_certificate" "site" {
  count             = local.custom_domain ? 1 : 0
  domain_name       = var.domain_name
  validation_method = "DNS"
  tags              = { Name = "${local.name}-site" }

  lifecycle {
    create_before_destroy = true
    # CloudFront only accepts certificates from us-east-1.
    precondition {
      condition     = var.region == "us-east-1"
      error_message = "A CloudFront certificate must be in us-east-1."
    }
  }
}

resource "aws_acm_certificate_validation" "site" {
  count           = local.custom_domain ? 1 : 0
  certificate_arn = aws_acm_certificate.site[0].arn

  timeouts {
    create = "45m"
  }
}
