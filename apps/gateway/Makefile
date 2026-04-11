.PHONY: proto build test lint clean docker compose-up compose-down

GATEWAY_BIN := bin/gateway
ENRICHER_BIN := bin/enricher

# Generate protobuf stubs from ../../proto using buf.
# Requires buf CLI: https://buf.build/docs/installation
proto:
	buf generate
	@echo "Proto stubs generated in pkg/proto/"

# Build both binaries.
build: $(GATEWAY_BIN) $(ENRICHER_BIN)

$(GATEWAY_BIN):
	go build -o $(GATEWAY_BIN) ./cmd/gateway

$(ENRICHER_BIN):
	go build -o $(ENRICHER_BIN) ./cmd/enricher

# Run unit tests (no containers).
test:
	go test -race -timeout 60s ./internal/... ./cmd/...

# Run integration tests (requires NATS + ClickHouse + MinIO via testcontainers).
test-integration:
	go test -race -timeout 300s -tags integration ./test/integration/...

# Run load test generator.
test-load:
	go test -timeout 60s ./test/load/...

# Lint with golangci-lint.
lint:
	golangci-lint run ./...

# Vet only (no external tools needed).
vet:
	go vet ./...

clean:
	rm -rf bin/ pkg/proto/personel

# Build Docker image for the gateway binary.
docker:
	docker build -t personel-gateway:latest -f Dockerfile .

# Start local dev stack.
compose-up:
	docker compose -f deployments/docker-compose-gateway.yaml up -d

# Stop local dev stack.
compose-down:
	docker compose -f deployments/docker-compose-gateway.yaml down

# Tidy go.mod / go.sum after adding dependencies.
tidy:
	go mod tidy
