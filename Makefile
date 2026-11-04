# Atlas – developer commands. Run `make help` for the list.
#
# Dev tools are pinned and installed into ./bin (git-ignored) by `make tools`,
# so they never clash with versions installed globally.

SHELL := /bin/bash
.DEFAULT_GOAL := help

export GOTOOLCHAIN := go1.27.3

BIN         := $(CURDIR)/bin
BINARY      := $(BIN)/atlas
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
COVERPROFILE := coverage.out

# Pinned tool versions (keep in sync with CI workflows).
SQLC_VERSION          := v1.31.1
MIGRATE_VERSION       := v4.20.1
GOLANGCI_LINT_VERSION := v2.15.0
GOSEC_VERSION         := v2.30.0
GOVULNCHECK_VERSION   := v1.9.0
GITLEAKS_VERSION      := v8.30.1
PRE_COMMIT_VERSION    := 4.7.0
HADOLINT_IMAGE        := hadolint/hadolint:v2.15.1

SQLC          := $(BIN)/sqlc
MIGRATE       := $(BIN)/migrate
GOLANGCI_LINT := $(BIN)/golangci-lint
GOSEC         := $(BIN)/gosec
GOVULNCHECK   := $(BIN)/govulncheck
GITLEAKS      := $(BIN)/gitleaks
PRE_COMMIT    := $(BIN)/.venv/bin/pre-commit

-include .env
export

##@ Development

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2} /^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)}' $(MAKEFILE_LIST)

.PHONY: run
run: ## Run the API locally (needs `make docker-up` for dependencies)
	go run .

.PHONY: build
build: ## Build the binary into ./bin/atlas
	CGO_ENABLED=1 go build -trimpath -tags netgo,osusergo -ldflags="$(LDFLAGS) -linkmode external -extldflags -static" -o $(BINARY) .

.PHONY: fmt
fmt: $(GOLANGCI_LINT) ## Format code (gofmt + goimports)
	$(GOLANGCI_LINT) fmt

.PHONY: generate
generate: sqlc swag ## Run every code generator (sqlc, swagger, mocks)
	go generate ./...

##@ Tests

.PHONY: test
test: ## Run unit tests
	go test ./...

.PHONY: test-race
test-race: ## Run unit tests with the race detector
	go test -race ./...

.PHONY: test-integration
test-integration: ## Run integration tests (needs Docker for testcontainers)
	go test -race -tags=integration ./...

.PHONY: test-e2e
test-e2e: ## Run the end-to-end flows against real Postgres, Redis and RabbitMQ (needs Docker)
	go test -race -tags=e2e -count=1 -timeout=15m ./test/e2e/...

.PHONY: test-r2
test-r2: ## Write, read and delete one test object in the real R2 bucket of .env
	go test -tags=r2live -count=1 -v ./infra/storage/r2/...

.PHONY: test-appypay
test-appypay: ## Create real test charges in the AppyPay test account of .env
	env $$(grep '^APPYPAY_' .env | xargs) go test -tags=appypaylive -count=1 -v ./infra/payment/appypay/...

.PHONY: test-load
test-load: ## Run the k6 load test against a disposable stack (needs Docker and k6)
	./test/load/run.sh

.PHONY: cover
cover: ## Run all tests with coverage and print the summary
	go test -race -tags=integration -covermode=atomic -coverprofile=$(COVERPROFILE) ./...
	go tool cover -func=$(COVERPROFILE) | tail -1

##@ Quality

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy
	go mod tidy -diff

.PHONY: security
security: $(GOSEC) $(GOVULNCHECK) $(GITLEAKS) ## Run gosec, govulncheck and gitleaks
	$(GOSEC) -quiet -exclude-generated -tags=integration ./...
	$(GOVULNCHECK) ./...
	$(GITLEAKS) git --redact --no-banner .

.PHONY: hadolint
hadolint: ## Lint the Dockerfile with hadolint (via Docker)
	docker run --rm -i $(HADOLINT_IMAGE) < Dockerfile

##@ Database

.PHONY: sqlc
sqlc: $(SQLC) ## Generate Go code from SQL queries
	$(SQLC) generate

.PHONY: sqlc-check
sqlc-check: $(SQLC) ## Fail if generated SQLC code is stale; vet queries
	$(SQLC) diff
	$(SQLC) vet

.PHONY: migrate-up
migrate-up: ## Apply all migrations
	go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	go run ./cmd/migrate down

.PHONY: migrate-create
migrate-create: $(MIGRATE) ## Create a migration pair: make migrate-create name=add_users
	@test -n "$(name)" || (echo "usage: make migrate-create name=<snake_case_name>" && exit 1)
	$(MIGRATE) create -ext sql -dir migrations -seq -digits 6 $(name)

.PHONY: seed
seed: ## Insert development seed data
	go run ./cmd/seed

.PHONY: bootstrap-admin
bootstrap-admin: ## Create the first admin if none exists (BOOTSTRAP_ADMIN_EMAIL/PHONE/PASSWORD)
	go run ./cmd/bootstrap-admin

##@ API docs

.PHONY: swag
swag: ## Generate the OpenAPI spec into docs/swagger
	go tool swag init -g main.go -o docs/swagger --parseInternal
	go tool swag fmt

##@ Docker

.PHONY: docker-up
docker-up: ## Start the full stack (builds the api image; the api applies migrations on start)
	docker compose up -d --build

.PHONY: docker-down
docker-down: ## Stop the stack (keeps volumes)
	docker compose down

.PHONY: docker-logs
docker-logs: ## Follow the api logs
	docker compose logs -f api

##@ Tooling

.PHONY: tools
tools: $(SQLC) $(MIGRATE) $(GOLANGCI_LINT) $(GOSEC) $(GOVULNCHECK) $(GITLEAKS) $(PRE_COMMIT) ## Install pinned dev tools into ./bin

.PHONY: pre-commit-install
pre-commit-install: $(PRE_COMMIT) ## Install the git hooks
	$(PRE_COMMIT) install --install-hooks

$(SQLC):
	GOBIN=$(BIN) go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

$(MIGRATE):
	GOBIN=$(BIN) go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

$(GOLANGCI_LINT):
	GOBIN=$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOSEC):
	GOBIN=$(BIN) go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)

$(GOVULNCHECK):
	GOBIN=$(BIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

$(GITLEAKS):
	GOBIN=$(BIN) go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)

# pre-commit lives in a project venv. Systems without ensurepip (Debian/Ubuntu
# without python3-venv) get pip through the official bootstrap script.
$(PRE_COMMIT):
	@if ! python3 -m venv $(BIN)/.venv >/dev/null 2>&1; then \
		rm -rf $(BIN)/.venv && python3 -m venv --without-pip $(BIN)/.venv && \
		curl -sSfL https://bootstrap.pypa.io/get-pip.py -o $(BIN)/get-pip.py && \
		$(BIN)/.venv/bin/python $(BIN)/get-pip.py -q && rm -f $(BIN)/get-pip.py; \
	fi
	$(BIN)/.venv/bin/pip install -q pre-commit==$(PRE_COMMIT_VERSION)
