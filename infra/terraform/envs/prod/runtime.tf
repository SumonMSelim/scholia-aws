# --- Tavily web search key ------------------------------------------------------
# An empty container. The owner puts the value in themselves (console or
# `aws secretsmanager put-secret-value`), so it never passes through Terraform,
# state, CI or a coding agent. Without a value, web search stays off.

resource "aws_secretsmanager_secret" "tavily" {
  #checkov:skip=CKV2_AWS_57:A third-party API key; rotate it in the Tavily dashboard, then put the new value.
  name                    = "scholia/tavily"
  description             = "Tavily API key for Scholia web search"
  kms_key_id              = aws_kms_key.main.arn
  recovery_window_in_days = 7
}

# --- Kill switch ----------------------------------------------------------------
# Flipped in the console or with `aws ssm put-parameter --overwrite` during an
# incident. Terraform sets the first value only, so an apply never undoes a flip.

resource "aws_ssm_parameter" "kill_switch" {
  #checkov:skip=CKV2_AWS_34:Feature flags, not a secret; String keeps reads free of KMS calls.
  name        = "/scholia/prod/kill-switch"
  description = "Run-time switches for server-paid models, guest sessions and uploads"
  type        = "String"
  value = jsonencode({
    server_models = true
    guests        = true
    uploads       = true
  })

  lifecycle {
    ignore_changes = [value]
  }
}
