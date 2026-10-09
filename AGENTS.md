# Scholia agent and contributor guide

Read [README.md](README.md) for what Scholia does and how it runs on AWS.

## Stack

- Go 1.27 for the API (`cmd/api`), the worker (`cmd/worker`) and the demo seed (`cmd/seed`); shared code in `internal/`.
- Terraform 1.16 with the AWS provider 6.x in `infra/terraform/`.
- React, Vite, TypeScript, Tailwind and shadcn/ui in `web/`.
- Docker Compose with floci for local AWS in `deploy/`.

## Conventions

- The API is a plain `http.Handler`; never import Lambda packages outside `cmd/`.
- AWS clients are created from config so `AWS_ENDPOINT_URL` can point at floci locally.
- External services (Bedrock, the vector store, web search) sit behind interfaces with fakes for tests.
- Every extracted span of content carries a source locator through to its chunk.
- Table-driven tests; the coverage gate is 85% on `internal/...` (`make cover`).
- Comments explain constraints and intent, not what the next line does.
- No secrets in code, logs or test fixtures. Validate all untrusted input at the boundary.
- Terraform: pin versions, least-privilege IAM, no resources without tags, `terraform fmt` clean.

## Commands

- `make ci` runs lint, tests, coverage gate, vulnerability and security scans, and builds.
- `make run-api` runs the API on port 8787.
