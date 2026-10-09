#!/usr/bin/env bash
# Builds the API and web app, applies the prod stack, publishes the web app and
# runs the smoke test against the public URL.
#
# Usage: AWS_PROFILE=<profile> scripts/deploy.sh [--auto-approve]
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
tf_dir="$root/infra/terraform/envs/prod"
region="${AWS_REGION:-us-east-1}"
approve="${1:-}"

account_id="$(aws sts get-caller-identity --query Account --output text)"
state_bucket="scholia-tfstate-${account_id}-${region}"

echo "==> building API"
make -C "$root" build

echo "==> building web app"
(cd "$root/web" && npm ci --no-audit --no-fund && npm run build)

echo "==> applying infrastructure"
terraform -chdir="$tf_dir" init -input=false -reconfigure -backend-config="bucket=${state_bucket}"
if [[ "$approve" == "--auto-approve" ]]; then
  terraform -chdir="$tf_dir" apply -input=false -auto-approve
else
  terraform -chdir="$tf_dir" apply -input=false
fi

url="$(terraform -chdir="$tf_dir" output -raw url)"
bucket="$(terraform -chdir="$tf_dir" output -raw site_bucket)"
distribution="$(terraform -chdir="$tf_dir" output -raw distribution_id)"

echo "==> publishing web app to s3://${bucket}"
# Hashed assets never change, so they can be cached forever; everything else must revalidate.
aws s3 sync "$root/web/dist/assets" "s3://${bucket}/assets" --delete \
  --cache-control "public, max-age=31536000, immutable"
aws s3 sync "$root/web/dist" "s3://${bucket}" --delete --exclude "assets/*" \
  --cache-control "no-cache"
aws cloudfront create-invalidation --distribution-id "$distribution" --paths "/" "/index.html" >/dev/null

echo "==> smoke testing ${url}"
"$root/scripts/smoke.sh" "$url"
