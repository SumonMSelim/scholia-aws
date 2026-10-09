# Account-wide default. It sits in bootstrap because it covers the whole
# account, not one stack, and the account has no organization to set it.

# Every bucket in the account blocks public access on its own; this makes it the default.
resource "aws_s3_account_public_access_block" "this" {
  block_public_acls       = true
  ignore_public_acls      = true
  block_public_policy     = true
  restrict_public_buckets = true
}
