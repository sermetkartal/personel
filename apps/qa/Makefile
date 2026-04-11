# Makefile for the Personel QA framework.
# Run `make help` to see available targets.

.PHONY: help build test unit e2e security load-500 load-10k fuzz probe redteam clean

GATEWAY_ADDR ?= localhost:9443
REPORT_DIR   ?= /tmp/reports

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} \
	/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build all commands
	go build ./...
	go build ./cmd/simulator/...
	go build ./cmd/probe/...
	go build ./cmd/audit-redteam/...
	go build ./cmd/chaos/...
	@echo "Build OK (footprint-bench requires Windows; skipped)"

test: unit e2e ## Run unit + e2e tests

unit: ## Run unit tests (fast, no containers)
	go test -v -short -timeout 5m \
		./internal/simulator/... \
		./internal/assertions/... \
		./test/security/... \
		-count=1

e2e: ## Run e2e tests (requires QA_INTEGRATION=1)
	QA_INTEGRATION=1 \
	GATEWAY_ADDR=$(GATEWAY_ADDR) \
	go test -v -timeout 20m ./test/e2e/... -count=1

security: ## Run security tests including red team (EC-9)
	@ci/scripts/run-security-suite.sh

fuzz: ## Run fuzz tests (60 seconds each)
	go test -fuzz=FuzzAgentMessageParsing -fuzztime=60s ./test/security/fuzz/...
	go test -fuzz=FuzzServerMessageParsing -fuzztime=60s ./test/security/fuzz/...
	go test -fuzz=FuzzEventBatchParsing -fuzztime=60s ./test/security/fuzz/...

probe: ## Run single-agent smoke probe against GATEWAY_ADDR
	go build -o /tmp/personel-probe ./cmd/probe/...
	/tmp/personel-probe --gateway $(GATEWAY_ADDR) --verbose

redteam: ## Run keystroke admin-blindness red team (EC-9)
	go build -o /tmp/personel-redteam ./cmd/audit-redteam/...
	/tmp/personel-redteam --api http://$(GATEWAY_ADDR) --verbose

load-500: ## Run 500-agent load test (~35 min)
	GATEWAY_ADDR=$(GATEWAY_ADDR) REPORT_DIR=$(REPORT_DIR) ci/scripts/run-load-500.sh

load-10k: ## Run 10K-agent load test (~45 min)
	GATEWAY_ADDR=$(GATEWAY_ADDR) REPORT_DIR=$(REPORT_DIR) ci/scripts/run-load-10k.sh

load-10k-burst: ## Run 10K-agent burst scenario
	GATEWAY_ADDR=$(GATEWAY_ADDR) SCENARIO=burst REPORT_DIR=$(REPORT_DIR) ci/scripts/run-load-10k.sh

load-chaos: ## Run 10K chaos mix scenario
	GATEWAY_ADDR=$(GATEWAY_ADDR) SCENARIO=chaos REPORT_DIR=$(REPORT_DIR) ci/scripts/run-load-10k.sh

phase1-report: ## Generate Phase 1 Exit Criteria report
	ci/scripts/generate-phase1-exit-report.sh $(REPORT_DIR) $(REPORT_DIR)/exit-report

clean: ## Remove build artifacts
	rm -f /tmp/personel-simulator /tmp/personel-probe /tmp/personel-redteam
	rm -rf /tmp/reports
