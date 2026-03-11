.PHONY: all fmt lint test test-go test-py build compose-up compose-down clean proto

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

# ─── Cleanup ─────────────────────────────────────────────────────────────────

clean:
	rm -rf bin/
