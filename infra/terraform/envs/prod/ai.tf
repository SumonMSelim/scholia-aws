# --- Vector store ---------------------------------------------------------------
# The API queries the index named after the embedding model (vector.IndexName).
# A query against a missing index is an error, not an empty result, so the index
# exists from the first apply. The code creates an index on first write too, and
# accepts an existing one when the dimension matches.

locals {
  # vector.IndexName: lowercase, anything outside [a-z0-9.-] becomes a hyphen.
  vector_index = trim(replace(lower(var.embed_model), "/[^a-z0-9.-]/", "-"), "-.")
}

# SSE-S3, not the stack's CMK: SSE-KMS on a vector bucket also needs a key-policy
# grant to the S3 Vectors indexing principal. Vectors are derived from course
# files that are already KMS-encrypted in the uploads bucket.
resource "aws_s3vectors_vector_bucket" "main" {
  vector_bucket_name = "${local.name}-vectors-${local.account_id}"

  encryption_configuration {
    sse_type = "AES256"
  }
}

resource "aws_s3vectors_index" "embed" {
  vector_bucket_name = aws_s3vectors_vector_bucket.main.vector_bucket_name
  index_name         = local.vector_index
  data_type          = "float32"
  dimension          = var.embed_dimension
  distance_metric    = "cosine"

  # locator is returned with a hit and is not a query filter.
  metadata_configuration {
    non_filterable_metadata_keys = ["locator"]
  }
}

# --- Guardrail ------------------------------------------------------------------
# A second layer behind the classifier in Go. The code still decides what is on
# topic; the guardrail blocks harmful content and prompt attacks at the model.

resource "aws_bedrock_guardrail" "main" {
  name                      = "${local.name}-study"
  description               = "Content and prompt-attack filters for the Scholia study assistant"
  blocked_input_messaging   = "I can't help with that. Let's keep to your course."
  blocked_outputs_messaging = "I can't share that response."
  # An AWS owned key. A CMK would need kms:Decrypt for every caller of the guardrail,
  # and the configuration holds filter settings only, no user data.

  content_policy_config {
    dynamic "filters_config" {
      for_each = ["HATE", "INSULTS", "SEXUAL", "VIOLENCE"]
      content {
        type            = filters_config.value
        input_strength  = "HIGH"
        output_strength = "HIGH"
      }
    }

    # Security courses ask about attacks: "How does ARP spoofing enable a
    # man-in-the-middle attack?" scores MISCONDUCT at HIGH confidence, which every
    # strength but NONE blocks. Harmful requests are left to the classifier and the
    # denied topics; the output filter still blocks step-by-step exploit text.
    filters_config {
      type            = "MISCONDUCT"
      input_strength  = "NONE"
      output_strength = "HIGH"
    }

    # Prompt attacks are an input concern; output must be NONE. LOW blocks only
    # HIGH-confidence hits: "5 questions, one at a time, then grade my answers"
    # scores LOW or MEDIUM depending on a trailing period, while real attempts to
    # override the rules score HIGH. The classifier also screens for jailbreaks.
    filters_config {
      type            = "PROMPT_ATTACK"
      input_strength  = "LOW"
      output_strength = "NONE"
    }
  }

  topic_policy_config {
    topics_config {
      name       = "weapons-and-explosives"
      type       = "DENY"
      definition = "Instructions for making, acquiring or using weapons, explosives or other devices meant to injure people."
      examples   = ["How do I build a pipe bomb?", "Give me steps to make a gun untraceable."]
    }

    topics_config {
      name       = "illegal-drug-production"
      type       = "DENY"
      definition = "Instructions for producing, extracting or trafficking illegal drugs."
      examples   = ["How do I synthesize methamphetamine at home?"]
    }

    topics_config {
      name       = "cheating-live-assessment"
      type       = "DENY"
      definition = "Help to cheat in a live proctored exam, such as evading proctoring software or getting answers passed in during the exam."
      examples   = ["How do I hide my phone from the exam proctor camera?"]
    }
  }

  word_policy_config {
    managed_word_lists_config {
      type = "PROFANITY"
    }
  }
}

# A new version is published whenever the guardrail changes. Old versions are
# kept so a running function never points at a deleted version.
resource "aws_bedrock_guardrail_version" "main" {
  guardrail_arn = aws_bedrock_guardrail.main.guardrail_arn
  description   = "Published by Terraform"
  skip_destroy  = true

  lifecycle {
    replace_triggered_by = [aws_bedrock_guardrail.main]
  }
}

# --- Bedrock access for the API -------------------------------------------------

locals {
  # A US inference profile routes to the foundation model in any of its regions,
  # so the role needs the profile and the model in every destination region.
  server_foundation_models = [for m in var.server_models : trimprefix(m, "us.")]

  bedrock_invoke_resources = concat(
    [for m in var.server_models : "arn:aws:bedrock:${var.region}:${local.account_id}:inference-profile/${m}"],
    flatten([for r in var.bedrock_regions : [for m in local.server_foundation_models : "arn:aws:bedrock:${r}::foundation-model/${m}"]]),
    ["arn:aws:bedrock:${var.region}::foundation-model/${var.embed_model}"],
  )
}
