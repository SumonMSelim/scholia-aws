SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

GOLANGCI_LINT_VERSION := v2.14.0
COVERAGE_MIN          ?= 85
BIN                   := $(CURDIR)/bin
DIST                  := $(CURDIR)/dist
VERSION               ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT                ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS               := -s -w \
	-X github.com/sumonmselim/scholia-aws/internal/version.Version=$(VERSION) \
	-X github.com/sumonmselim/scholia-aws/internal/version.Commit=$(COMMIT)

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

$(BIN)/golangci-lint:
	@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(BIN) $(GOLANGCI_LINT_VERSION)

.PHONY: fmt
fmt: ## Format Go and Terraform code
	gofmt -w cmd internal
	terraform fmt -recursive infra 2>/dev/null || true

.PHONY: lint
lint: $(BIN)/golangci-lint ## Run golangci-lint
	$(BIN)/golangci-lint run ./...

.PHONY: test
test: ## Run unit tests with the race detector
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests and enforce the coverage gate on internal packages
	go test -count=1 -covermode=atomic -coverprofile=coverage.out ./internal/...
	@scripts/coverage-gate.sh coverage.out $(COVERAGE_MIN)

FUZZ_TIME ?= 10s

.PHONY: fuzz
fuzz: ## Fuzz untrusted parsers for a bounded time
	go test -fuzz=FuzzVTT -fuzztime=$(FUZZ_TIME) ./internal/transcript/
	go test -fuzz=FuzzSRT -fuzztime=$(FUZZ_TIME) ./internal/transcript/
	go test -fuzz=FuzzTranscribeJSON -fuzztime=$(FUZZ_TIME) ./internal/transcript/
	go test -fuzz=FuzzPDF -fuzztime=$(FUZZ_TIME) ./internal/pdfdoc/
	go test -fuzz=FuzzPPTX -fuzztime=$(FUZZ_TIME) ./internal/pptx/

.PHONY: vuln
vuln: ## Scan dependencies for known vulnerabilities
	go tool govulncheck ./...

.PHONY: sec
sec: ## Static security analysis
	go tool gosec -quiet ./...

.PHONY: build
build: ## Build Lambda binaries for linux/arm64
	@for cmd in $$(ls cmd); do \
		echo "building $$cmd"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -tags lambda.norpc \
			-ldflags "$(LDFLAGS)" -o $(DIST)/$$cmd/bootstrap ./cmd/$$cmd || exit 1; \
	done

.PHONY: seed-demo
seed-demo: ## Queue the public 6.006 demo course (needs AWS credentials, TABLE_NAME, UPLOADS_BUCKET)
	go run ./cmd/seed

.PHONY: run-api
run-api: ## Run the API locally
	go run -ldflags "$(LDFLAGS)" ./cmd/api

.PHONY: web
web: ## Test and build the web app
	npm ci --prefix web --no-audit --no-fund
	npm test --prefix web
	npm run build --prefix web

.PHONY: tf
tf: ## Format-check, lint, scan and test Terraform
	@command -v terraform >/dev/null || { echo "terraform is required" >&2; exit 1; }
	@command -v tflint >/dev/null || { echo "tflint is required" >&2; exit 1; }
	@command -v checkov >/dev/null || { echo "checkov is required" >&2; exit 1; }
	terraform fmt -check -recursive infra/terraform
	tflint --init --chdir=infra/terraform
	tflint --recursive --chdir=infra/terraform
	@for dir in infra/terraform/bootstrap infra/terraform/envs/prod infra/terraform/modules/edge infra/terraform/modules/lambda_function; do \
		echo "==> terraform test $$dir"; \
		terraform -chdir="$$dir" init -backend=false -input=false >/dev/null && \
		terraform -chdir="$$dir" test || exit 1; \
	done
	checkov -d infra/terraform --framework terraform --compact --quiet

.PHONY: trivy
trivy: ## Fail on high or critical vulnerabilities and leaked secrets
	@command -v trivy >/dev/null || { echo "trivy is required" >&2; exit 1; }
	trivy fs --scanners vuln,secret --severity HIGH,CRITICAL --exit-code 1 --ignore-unfixed .

.PHONY: ci
ci: lint test cover fuzz vuln sec build web tf trivy ## Everything the pipeline runs

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(DIST) coverage.out
