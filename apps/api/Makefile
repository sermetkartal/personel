# Makefile — Personel Admin API
#
# Targets:
#   build         Build the binary
#   run           Run with local config (requires docker-compose services)
#   test          Run unit tests
#   test-int      Run integration tests (requires Docker)
#   test-all      Unit + integration
#   lint          Run golangci-lint
#   vet           Run go vet
#   fmt           Format code (goimports)
#   check         vet + lint (CI gate)
#   migrate-up    Apply all pending Postgres migrations
#   migrate-down  Revert last Postgres migration
#   openapi-lint  Validate OpenAPI spec
#   docker-build  Build Docker image
#   clean         Remove build artifacts

BINARY        := personel-api
CMD_PATH      := ./cmd/api
BUILD_DIR     := .build
GOFLAGS       := -trimpath
LDFLAGS       := -s -w \
                 -X main.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev") \
                 -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Docker
IMAGE_NAME    := personel/api
IMAGE_TAG     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Postgres (for migration targets)
PG_DSN        ?= postgres://personel:personel@localhost:5432/personel?sslmode=disable

# golangci-lint version
LINT_VERSION  := v1.59.1

# Go tools
GO            := go
GOIMPORTS     := $(shell which goimports 2>/dev/null || echo "$(go env GOPATH)/bin/goimports")

.PHONY: all build run test test-int test-all lint vet fmt check \
        migrate-up migrate-down openapi-lint docker-build clean \
        tools audit-actions-check

all: check build

# ── Build ────────────────────────────────────────────────────────────────────

build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

run: build
	PERSONEL_CONFIG_PATH=configs/api.yaml $(BUILD_DIR)/$(BINARY)

# ── Test ─────────────────────────────────────────────────────────────────────

test:
	$(GO) test -race -count=1 -timeout=120s ./internal/...

# Integration tests require Docker (testcontainers-go spins up Postgres, ClickHouse, NATS, MinIO).
test-int:
	$(GO) test -race -count=1 -timeout=300s -tags=integration ./test/integration/...

test-all: test test-int

# Generate and show test coverage report.
cover:
	@mkdir -p $(BUILD_DIR)
	$(GO) test -race -coverprofile=$(BUILD_DIR)/coverage.out -covermode=atomic ./internal/...
	$(GO) tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@$(GO) tool cover -func=$(BUILD_DIR)/coverage.out | tail -1

# ── Quality ──────────────────────────────────────────────────────────────────

vet:
	$(GO) vet ./...

fmt:
	@command -v goimports >/dev/null 2>&1 || { \
		echo "Installing goimports..."; \
		$(GO) install golang.org/x/tools/cmd/goimports@latest; \
	}
	goimports -w -local github.com/personel/api ./cmd ./internal ./test

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "Installing golangci-lint $(LINT_VERSION)..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
			| sh -s -- -b $(shell go env GOPATH)/bin $(LINT_VERSION); \
	}
	golangci-lint run --timeout=5m ./...

# CI gate: must pass before merge.
check: vet audit-actions-check
	@echo "check passed"

# Verify audit AllActions slice is exhaustive (must be run as part of CI).
audit-actions-check:
	$(GO) run ./internal/audit/cmd/check-actions/... 2>/dev/null || \
		$(GO) test -run TestAllActionsExhaustive ./internal/audit/...

# ── Migrations ───────────────────────────────────────────────────────────────

migrate-up:
	@command -v migrate >/dev/null 2>&1 || { \
		echo "Installing golang-migrate CLI..."; \
		$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	}
	migrate -path internal/postgres/migrations -database "$(PG_DSN)" up
	@echo "Migrations applied."

migrate-down:
	@command -v migrate >/dev/null 2>&1 || { \
		echo "Installing golang-migrate CLI..."; \
		$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest; \
	}
	migrate -path internal/postgres/migrations -database "$(PG_DSN)" down 1
	@echo "Last migration reverted."

migrate-status:
	migrate -path internal/postgres/migrations -database "$(PG_DSN)" version

# ── OpenAPI ───────────────────────────────────────────────────────────────────

openapi-lint:
	@command -v redocly >/dev/null 2>&1 || { \
		echo "Installing @redocly/cli..."; \
		npm install -g @redocly/cli@latest; \
	}
	redocly lint api/openapi.yaml
	@echo "OpenAPI spec is valid."

# ── Docker ───────────────────────────────────────────────────────────────────

docker-build:
	docker build \
		--build-arg VERSION=$(IMAGE_TAG) \
		--build-arg BUILD_TIME=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(IMAGE_NAME):latest \
		-f Dockerfile \
		.
	@echo "Image: $(IMAGE_NAME):$(IMAGE_TAG)"

# ── Tools ─────────────────────────────────────────────────────────────────────

tools:
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "Tools installed."

# ── Tidy ─────────────────────────────────────────────────────────────────────

tidy:
	$(GO) mod tidy

# ── Clean ─────────────────────────────────────────────────────────────────────

clean:
	rm -rf $(BUILD_DIR)
	@echo "Cleaned."
