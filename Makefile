# GitShop Makefile
# Simple commands run on host, Docker commands prefixed with 'docker.'

.PHONY: help
.DEFAULT_GOAL := help

DC ?= docker compose
RUN_DEV := $(DC) run --rm gitshop-dev
RUN_MIGRATE := $(DC) run --rm migrate
MIGRATE_DB ?= postgres://gitshop:gitshop@postgres:5432/gitshop?sslmode=disable
MIGRATE_ARGS := -path=/migrations/ -database $(MIGRATE_DB)
PROD_IMAGE_TAG ?= gitshop:prod
GOLANGCI_LINT_VERSION ?= v2.8.0
SQLC_VERSION ?= v1.30.0
TEMPL_VERSION ?= v0.3.977
GO_CACHE_DIR ?= $(CURDIR)/tmp/go-build-cache
GOLANGCI_CACHE_DIR ?= $(CURDIR)/tmp/golangci-lint-cache
GO_ENV := GOCACHE=$(GO_CACHE_DIR)
LINT_ENV := GOCACHE=$(GO_CACHE_DIR) GOLANGCI_LINT_CACHE=$(GOLANGCI_CACHE_DIR)

# =============================================================================
# Host Commands (run on your machine - requires: go, sqlc, templ, npm, golangci-lint)
# =============================================================================

.PHONY: run build test test-coverage test-coverage-ci lint lint-fix generate clean
.PHONY: install-tools
.PHONY: ui.build ui.watch

# Run the application locally
run:
	$(GO_ENV) go run ./cmd/server/main.go

# Build the application locally
build: generate
	$(GO_ENV) go build -o ./bin/gitshop ./cmd/server/main.go

# Install local dev/CI tools at pinned versions
install-tools:
	$(GO_ENV) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO_ENV) go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	$(GO_ENV) go install github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)

# Run tests locally
test:
	$(GO_ENV) go test -v ./...

# Run tests with CI coverage settings
test-coverage-ci:
	$(GO_ENV) go test -v -covermode=atomic -coverprofile=coverage.out ./...

# Run tests with coverage locally
test-coverage: test-coverage-ci
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter locally
lint:
	@mkdir -p "$(GO_CACHE_DIR)" "$(GOLANGCI_CACHE_DIR)"
	@set -e; \
	output_file=$$(mktemp); \
	if ! $(LINT_ENV) golangci-lint run ./... >"$$output_file" 2>&1; then \
		cat "$$output_file"; \
		if grep -q "no go files to analyze" "$$output_file"; then \
			echo "golangci-lint could not analyze packages in this environment; falling back to go vet"; \
			$(GO_ENV) go vet ./...; \
		else \
			rm -f "$$output_file"; \
			exit 1; \
		fi; \
	else \
		cat "$$output_file"; \
	fi; \
	rm -f "$$output_file"

# Run linter with auto-fix locally
lint-fix:
	@mkdir -p "$(GO_CACHE_DIR)" "$(GOLANGCI_CACHE_DIR)"
	$(LINT_ENV) golangci-lint run --fix ./...

# Generate all generated code (sqlc + templ)
generate: sqlc templ

# Generate sqlc code locally
sqlc:
	sqlc generate

# Generate templ templates locally
templ:
	templ generate

# Build Tailwind CSS for templUI
ui.build:
	npm run ui:build

# Watch Tailwind CSS for changes
ui.watch:
	npm run ui:watch

# Clean build artifacts locally
clean:
	rm -rf ./bin/gitshop
	rm -f coverage.out coverage.html
	rm -rf "$(GO_CACHE_DIR)" "$(GOLANGCI_CACHE_DIR)"
	$(GO_ENV) go clean -cache

# =============================================================================
# Docker Commands (run in Docker container)
# =============================================================================

.PHONY: docker.up docker.down docker.build docker.prod.build docker.up.d
.PHONY: docker.test docker.test-coverage docker.lint docker.lint-fix
.PHONY: docker.sqlc docker.templ docker.dev-setup
.PHONY: docker.go.build docker.ui.build docker.ui.watch
.PHONY: docker.psql docker.logs docker.clean docker.prune.global
.PHONY: docker.migrate.up docker.migrate.down docker.migrate.drop docker.migrate.force

# Start all services
docker.up:
	$(DC) up --build

# Stop all services
docker.down:
	$(DC) down

# Build Docker images and app artifacts
docker.build:
	$(DC) build
	$(MAKE) docker.ui.build
	$(MAKE) docker.go.build

# Build production image stage only
docker.prod.build:
	docker build --target prod -t $(PROD_IMAGE_TAG) .

# Start services in background
docker.up.d:
	$(DC) up -d

# Run tests in Docker
docker.test:
	$(RUN_DEV) make test

# Run tests with coverage in Docker
docker.test-coverage:
	$(RUN_DEV) make test-coverage

# Run linter in Docker
docker.lint:
	$(RUN_DEV) make lint

# Run linter with auto-fix in Docker
docker.lint-fix:
	$(RUN_DEV) make lint-fix

# Run sqlc in Docker
docker.sqlc:
	$(RUN_DEV) make sqlc

# Generate templates in Docker
docker.templ:
	$(RUN_DEV) make templ

# Build Go binary in Docker
docker.go.build:
	$(RUN_DEV) make build

# Build Tailwind CSS in Docker
docker.ui.build:
	$(RUN_DEV) make ui.build

# Watch Tailwind CSS in Docker
docker.ui.watch:
	$(RUN_DEV) make ui.watch

# Full development setup in Docker
docker.dev-setup:
	@echo "Setting up development environment..."
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env file - please update with your values"; fi
	$(MAKE) docker.migrate.up
	@echo "Development setup complete!"

# Run database migrations in Docker
docker.migrate.up:
	$(RUN_MIGRATE) $(MIGRATE_ARGS) up

# Rollback last migration in Docker
docker.migrate.down:
	$(RUN_MIGRATE) $(MIGRATE_ARGS) down 1

# Drop all migrations (WARNING: destroys all data)
docker.migrate.drop:
	$(RUN_MIGRATE) $(MIGRATE_ARGS) drop

# Force migration version in Docker
docker.migrate.force:
	@if [ -z "$(V)" ]; then echo "Usage: make docker.migrate.force V=<version>"; exit 1; fi
	$(RUN_MIGRATE) $(MIGRATE_ARGS) force $(V)

# Connect to PostgreSQL
docker.psql:
	$(DC) exec postgres psql -U gitshop gitshop

# View service logs
docker.logs:
	$(DC) logs -f gitshop-dev

# Clean up Docker resources
docker.clean:
	$(DC) down -v --remove-orphans

# Global Docker prune (WARNING: affects resources outside this project)
docker.prune.global:
	docker system prune -f

# =============================================================================
# Short Aliases
# =============================================================================

.PHONY: dup dt dlint dbuild drun dlogs ddown dclean dprune
.PHONY: dmup dmdown dmdrop dmforce
.PHONY: dpsql

dup: docker.up.d
dt: docker.test
dlint: docker.lint
dbuild: docker.build
drun: docker.up
dlogs: docker.logs
ddown: docker.down
dclean: docker.clean
dprune: docker.prune.global

dmup: docker.migrate.up
dmdown: docker.migrate.down
dmdrop: docker.migrate.drop
dmforce: docker.migrate.force

dpsql: docker.psql

# =============================================================================
# Help
# =============================================================================

help:
	@echo "GitShop Makefile"
	@echo ""
	@echo "=== Host Commands ==="
	@echo "  make run               - Run application locally"
	@echo "  make build             - Build application locally"
	@echo "  make install-tools     - Install pinned local/CI toolchain"
	@echo "  make test              - Run tests locally"
	@echo "  make test-coverage-ci  - Run tests with CI coverage settings"
	@echo "  make test-coverage     - Run tests with coverage"
	@echo "  make lint              - Run linter locally"
	@echo "  make lint-fix          - Run linter with auto-fix"
	@echo "  make generate          - Generate sqlc + templ code"
	@echo "  make ui.build          - Build Tailwind CSS for templUI"
	@echo "  make ui.watch          - Watch Tailwind CSS for changes"
	@echo "  make clean             - Clean build artifacts"
	@echo ""
	@echo "=== Docker Commands ==="
	@echo "  make docker.build      - Build Docker image + UI CSS + Go binary"
	@echo "  make docker.prod.build - Build production Docker image stage"
	@echo "  make docker.up         - Start all services"
	@echo "  make docker.down       - Stop all services"
	@echo "  make docker.up.d       - Start services in background"
	@echo "  make docker.go.build   - Build Go binary in Docker"
	@echo "  make docker.templ      - Generate templ code in Docker"
	@echo "  make docker.ui.build   - Build Tailwind CSS in Docker"
	@echo "  make docker.ui.watch   - Watch Tailwind CSS in Docker"
	@echo "  make docker.test       - Run tests in Docker"
	@echo "  make docker.test-coverage - Run tests with coverage in Docker"
	@echo "  make docker.lint       - Run linter in Docker"
	@echo "  make docker.logs       - View service logs"
	@echo "  make docker.clean      - Clean project Docker resources"
	@echo "  make docker.prune.global - Global Docker prune (DANGER)"
	@echo ""
	@echo "=== Database Migrations (Docker) ==="
	@echo "  make docker.migrate.up     - Run migrations"
	@echo "  make docker.migrate.down   - Rollback last migration"
	@echo "  make docker.migrate.drop   - Drop all tables (DANGER!)"
	@echo "  make docker.migrate.force V=<ver> - Force version"
	@echo ""
	@echo "=== Short Aliases ==="
	@echo "  dup      = docker.up.d"
	@echo "  dt       = docker.test"
	@echo "  dlint    = docker.lint"
	@echo "  dbuild   = docker.build"
	@echo "  drun     = docker.up"
	@echo "  dlogs    = docker.logs"
	@echo "  ddown    = docker.down"
	@echo "  dclean   = docker.clean"
	@echo "  dprune   = docker.prune.global"
	@echo "  dmup     = docker.migrate.up"
	@echo "  dmdown   = docker.migrate.down"
	@echo "  dmdrop   = docker.migrate.drop"
	@echo "  dmforce  = docker.migrate.force"
