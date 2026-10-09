# The bucket is created by this stack on first run with local state (see
# scripts/bootstrap.sh), after which the state is migrated into it.
terraform {
  backend "s3" {
    key          = "bootstrap/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}
