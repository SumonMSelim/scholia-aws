# --- Email sign-in --------------------------------------------------------------
# internal/auth runs USER_AUTH with the EMAIL_OTP challenge and sends no client
# secret. Passwordless sign-in needs the Essentials tier. Cognito requires
# PASSWORD in the allowed first factors, so it stays listed; the app never
# offers it.

resource "aws_cognito_user_pool" "main" {
  #checkov:skip=CKV_AWS_362:Advanced security is a Plus-tier feature; sign-in is by emailed one-time code only.
  name                     = local.name
  user_pool_tier           = "ESSENTIALS"
  deletion_protection      = "ACTIVE"
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]
  mfa_configuration        = "OFF"

  sign_in_policy {
    allowed_first_auth_factors = ["EMAIL_OTP", "PASSWORD"]
  }

  username_configuration {
    case_sensitive = false
  }

  password_policy {
    minimum_length                   = 14
    require_lowercase                = true
    require_uppercase                = true
    require_numbers                  = true
    require_symbols                  = true
    temporary_password_validity_days = 1
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  admin_create_user_config {
    allow_admin_create_user_only = false
  }

  # The Cognito default sender has a low daily limit. A verified SES identity
  # lifts it; SES must also be out of its sandbox to reach any address.
  email_configuration {
    email_sending_account = var.ses_from_address == null ? "COGNITO_DEFAULT" : "DEVELOPER"
    from_email_address    = var.ses_from_address
    source_arn            = var.ses_from_address == null ? null : "arn:aws:ses:${var.region}:${local.account_id}:identity/${var.ses_from_address}"
  }
}

resource "aws_cognito_user_pool_client" "web" {
  name                = "${local.name}-web"
  user_pool_id        = aws_cognito_user_pool.main.id
  generate_secret     = false
  explicit_auth_flows = ["ALLOW_USER_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"]
  # LEGACY lets the API see UserNotFoundException and sign a new address up.
  # Only the API calls Cognito, and it returns the same response either way,
  # so callers still cannot tell whether an address has an account.
  prevent_user_existence_errors = "LEGACY"
  enable_token_revocation       = true
  auth_session_validity         = 5
  access_token_validity         = 60
  id_token_validity             = 60
  refresh_token_validity        = 7

  token_validity_units {
    access_token  = "minutes"
    id_token      = "minutes"
    refresh_token = "days"
  }
}

# --- Session signing secret -----------------------------------------------------
# Signs the app's own session tokens, for signed-in users and guests alike.
# The value is generated at apply and written with a write-only argument, so it
# is never stored in Terraform state or plan output. Bump the version to rotate.

resource "aws_secretsmanager_secret" "session" {
  #checkov:skip=CKV2_AWS_57:Rotation is manual: bump secret_string_wo_version. Rotating signs out every session.
  name                    = "scholia/session"
  description             = "Signing secret for Scholia session tokens"
  kms_key_id              = aws_kms_key.main.arn
  recovery_window_in_days = 7
}

ephemeral "random_password" "session" {
  length  = 64
  special = false
}

resource "aws_secretsmanager_secret_version" "session" {
  secret_id                = aws_secretsmanager_secret.session.id
  secret_string_wo         = ephemeral.random_password.session.result
  secret_string_wo_version = 1
}
