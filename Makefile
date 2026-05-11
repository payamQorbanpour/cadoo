SHELL := /bin/bash
.DEFAULT_GOAL := help

GO ?= go
BIN_DIR := bin
LDFLAGS := -s -w -X github.com/payamqorbanpour/cadoo/internal/version.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

CMDS := cadoo-api cadoo-webhook cadoo-worker cadoo-cli cadoo-tunnel

DOCKER_COMPOSE := docker compose -f deploy/docker/docker-compose.yml

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: build
build: ## Build all binaries to ./bin
	@mkdir -p $(BIN_DIR)
	@for cmd in $(CMDS); do \
		echo "  build $$cmd"; \
		$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$$cmd ./cmd/$$cmd || exit 1; \
	done

.PHONY: test
test: ## Run unit tests
	$(GO) test -race -count=1 ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## golangci-lint (installs locally if missing)
	@which golangci-lint >/dev/null 2>&1 || $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run ./...

.PHONY: tools-install
tools-install: ## Install dev tools (sqlc, goose, golangci-lint)
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: sqlc
sqlc: ## Regenerate sqlc-generated query code
	sqlc generate

.PHONY: migrate
migrate: ## Apply all pending migrations against $$DATABASE_URL
	goose -dir db/migrations postgres "$$DATABASE_URL" up

.PHONY: migrate-down
migrate-down: ## Roll back one migration
	goose -dir db/migrations postgres "$$DATABASE_URL" down

.PHONY: dev-up
dev-up: ## Boot dev stack (postgres + litellm + services)
	$(DOCKER_COMPOSE) up -d --build

.PHONY: dev-down
dev-down: ## Tear down dev stack
	$(DOCKER_COMPOSE) down -v

.PHONY: dev-logs
dev-logs: ## Tail dev stack logs
	$(DOCKER_COMPOSE) logs -f

DOCKER_CLI_IMAGE ?= ghcr.io/payamqorbanpour/cadoo-cli:latest

.PHONY: docker-cli
docker-cli: ## Build cadoo-cli image for GitLab-CI-mode usage
	docker build -f deploy/docker/Dockerfile.cli -t $(DOCKER_CLI_IMAGE) .

.PHONY: docker-cli-push
docker-cli-push: docker-cli ## Push the cadoo-cli image to its registry
	docker push $(DOCKER_CLI_IMAGE)

.PHONY: ci
ci: vet test build ## What CI runs
