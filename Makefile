.PHONY: all fmt lint test test-go test-py test-contracts test-agent test-replay build compose-up compose-down compose-logs clean proto eval bench help

WORKER_DIR := worker
GO_PACKAGES := ./...

all: fmt lint test

# ─── Formatting ──────────────────────────────────────────────────────────────

fmt:
	go fmt $(GO_PACKAGES)
	cd $(WORKER_DIR) && ruff format .

# ─── Linting ─────────────────────────────────────────────────────────────────

lint: lint-go lint-py

lint-go:
	go vet $(GO_PACKAGES)
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, running go vet only"; \
	fi

lint-py:
	cd $(WORKER_DIR) && ruff check .
	cd $(WORKER_DIR) && mypy koala_worker

# ─── Testing ─────────────────────────────────────────────────────────────────

test: test-go test-py

test-go:
	go test -race -timeout 60s $(GO_PACKAGES)

test-py:
	cd $(WORKER_DIR) && python -m pytest -v

# Contract tests: validate MCP schema fixtures
test-contracts:
	go test -run TestContract -race -timeout 60s $(GO_PACKAGES)

# Agent harness: simulate prompt-driven tool selection
test-agent:
	go test -run TestAgentHarness -race -v -timeout 30s ./internal/mcp/

# Replay e2e: precision/recall gate on fixture dataset
test-replay:
	go test -run TestReplay -race -v -timeout 120s ./tests/

# ─── Build ───────────────────────────────────────────────────────────────────

build:
	go build -o bin/koala-orchestrator ./cmd/koala-orchestrator
	go build -o bin/koala-packager ./cmd/koala-packager

# ─── Compose ─────────────────────────────────────────────────────────────────

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f

# ─── Proto generation ────────────────────────────────────────────────────────
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc
# Install: brew install protobuf && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#          go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

proto:
	protoc \
	  --proto_path=proto \
	  --go_out=proto/inferencev1 \
	  --go_opt=paths=source_relative \
	  --go-grpc_out=proto/inferencev1 \
	  --go-grpc_opt=paths=source_relative \
	  proto/koala_inference.proto
	cd $(WORKER_DIR) && python -m grpc_tools.protoc \
	  -I../proto \
	  --python_out=koala_worker/proto \
	  --grpc_python_out=koala_worker/proto \
	  ../proto/koala_inference.proto

# ─── Python evaluation runner ────────────────────────────────────────────────

eval:
	cd $(WORKER_DIR) && python -m tools.eval_runner \
	  --fixtures ../tests/fixtures/replay/front_door_cases.json

# ─── Benchmarks ──────────────────────────────────────────────────────────────

bench:
	go test -bench=. -benchmem -timeout 120s $(GO_PACKAGES)

# ─── Help ────────────────────────────────────────────────────────────────────

help:
	@echo "Koala Makefile targets:"
	@echo ""
	@echo "  make              Run fmt + lint + test (default)"
	@echo "  make fmt          Format Go and Python sources"
	@echo "  make lint         Run go vet + ruff + mypy"
	@echo "  make test         Run all Go and Python tests"
	@echo "  make test-go      Go tests only (with -race)"
	@echo "  make test-py      Python tests only (pytest)"
	@echo "  make test-contracts  MCP schema contract tests"
	@echo "  make test-agent   Agent harness simulation tests"
	@echo "  make test-replay  Replay e2e precision/recall gate"
	@echo "  make eval         Python detection eval runner"
	@echo "  make bench        Go benchmarks"
	@echo "  make build        Build binaries to bin/"
	@echo "  make compose-up   Start services with Docker Compose"
	@echo "  make compose-down Stop Docker Compose services"
	@echo "  make compose-logs Tail Docker Compose logs"
	@echo "  make proto        Regenerate gRPC stubs"
	@echo "  make clean        Remove build artifacts"

# ─── Cleanup ─────────────────────────────────────────────────────────────────

clean:
	rm -rf bin/
