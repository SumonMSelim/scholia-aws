#!/usr/bin/env bash
# Creates the Terraform state bucket and budget, then moves the bootstrap
# stack's own state into that bucket. Safe to re-run.
#
# Usage: BUDGET_EMAIL=you@example.com AWS_PROFILE=<profile> scripts/bootstrap.sh
set -euo pipefail

: "${BUDGET_EMAIL:?BUDGET_EMAIL is required}"
cd "$(dirname "$0")/../infra/terraform/bootstrap"

account_id="$(aws sts get-caller-identity --query Account --output text)"
region="${AWS_REGION:-us-east-1}"
bucket="scholia-tfstate-${account_id}-${region}"

if ! aws s3api head-bucket --bucket "$bucket" 2>/dev/null; then
  echo "state bucket $bucket not found; creating with local state first"
  printf 'terraform {\n  backend "local" {}\n}\n' >backend_override.tf
  trap 'rm -f backend_override.tf' EXIT
  terraform init -input=false -reconfigure
  terraform apply -input=false -auto-approve -var "budget_email=${BUDGET_EMAIL}" -var "region=${region}"
  rm -f backend_override.tf
  trap - EXIT
  terraform init -input=false -migrate-state -force-copy -backend-config="bucket=${bucket}"
  rm -f terraform.tfstate terraform.tfstate.backup
else
  terraform init -input=false -reconfigure -backend-config="bucket=${bucket}"
  terraform apply -input=false -var "budget_email=${BUDGET_EMAIL}" -var "region=${region}"
fi

echo "state bucket: ${bucket}"
