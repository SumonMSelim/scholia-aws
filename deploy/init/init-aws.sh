#!/usr/bin/env bash
# Creates the local AWS resources Scholia needs in floci. Idempotent.
set -euo pipefail

for i in $(seq 1 30); do
  if aws dynamodb list-tables >/dev/null 2>&1; then
    break
  fi
  if [[ "$i" == 30 ]]; then
    echo "floci did not become ready" >&2
    exit 1
  fi
  sleep 1
done

if ! aws dynamodb describe-table --table-name "$SCHOLIA_TABLE" >/dev/null 2>&1; then
  aws dynamodb create-table \
    --table-name "$SCHOLIA_TABLE" \
    --attribute-definitions AttributeName=pk,AttributeType=S AttributeName=sk,AttributeType=S \
    --key-schema AttributeName=pk,KeyType=HASH AttributeName=sk,KeyType=RANGE \
    --billing-mode PAY_PER_REQUEST >/dev/null
  echo "created table $SCHOLIA_TABLE"
fi

# Guest records carry expires_at. Production enables the same TTL attribute.
aws dynamodb update-time-to-live --table-name "$SCHOLIA_TABLE" \
  --time-to-live-specification Enabled=true,AttributeName=expires_at >/dev/null 2>&1 || \
  echo "dynamodb ttl is unavailable locally" >&2

if ! aws s3api head-bucket --bucket "$SCHOLIA_UPLOADS_BUCKET" >/dev/null 2>&1; then
  aws s3api create-bucket --bucket "$SCHOLIA_UPLOADS_BUCKET" >/dev/null
  echo "created bucket $SCHOLIA_UPLOADS_BUCKET"
fi

# The browser PUTs the presigned URL from another origin. The signature still authorizes the write.
aws s3api put-bucket-cors --bucket "$SCHOLIA_UPLOADS_BUCKET" --cors-configuration '{
  "CORSRules": [{
    "AllowedOrigins": ["*"],
    "AllowedMethods": ["PUT", "GET", "HEAD"],
    "AllowedHeaders": ["*"],
    "ExposeHeaders": ["ETag"]
  }]
}' >/dev/null

queue="${SCHOLIA_QUEUE:-scholia-local-uploads}"
dlq="${queue}-dlq"
aws sqs create-queue --queue-name "$dlq" >/dev/null
aws sqs create-queue --queue-name "$queue" >/dev/null
queue_url="$(aws sqs get-queue-url --queue-name "$queue" --query QueueUrl --output text)"
queue_arn="$(aws sqs get-queue-attributes --queue-url "$queue_url" --attribute-names QueueArn --query Attributes.QueueArn --output text)"
# Production uses an S3 event. floci may not deliver it; the worker accepts the same message body either way.
if ! aws s3api put-bucket-notification-configuration \
  --bucket "$SCHOLIA_UPLOADS_BUCKET" \
  --notification-configuration "{\"QueueConfigurations\":[{\"QueueArn\":\"${queue_arn}\",\"Events\":[\"s3:ObjectCreated:*\"]}]}"; then
  echo "s3 event delivery is unavailable locally" >&2
fi

# Index name is vector.IndexName("amazon.titan-embed-text-v2:0").
# Titan Text Embeddings V2 defaults to 1024 dimensions. Another model id is another index.
vector_bucket="${SCHOLIA_VECTOR_BUCKET:-scholia-local-vectors}"
index_name="amazon.titan-embed-text-v2-0"
if ! aws s3vectors get-vector-bucket --vector-bucket-name "$vector_bucket" >/dev/null 2>&1; then
  aws s3vectors create-vector-bucket --vector-bucket-name "$vector_bucket" >/dev/null
  echo "created vector bucket $vector_bucket"
fi
if ! aws s3vectors get-index --vector-bucket-name "$vector_bucket" --index-name "$index_name" >/dev/null 2>&1; then
  aws s3vectors create-index \
    --vector-bucket-name "$vector_bucket" \
    --index-name "$index_name" \
    --data-type float32 \
    --dimension 1024 \
    --distance-metric cosine \
    --metadata-configuration nonFilterableMetadataKeys=locator >/dev/null
  echo "created vector index $index_name"
fi

echo "local AWS resources ready"
