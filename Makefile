SHELL   := /bin/bash
BINARY  := bin/api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: help env build run test cover vet fmt lint tidy clean

help: ## Show the available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

env: ## Create .env from .env.example when it does not exist yet
	@test -f .env || cp .env.example .env
	@echo ".env is ready"

build: ## Compile the API binary into bin/
	@mkdir -p bin
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/api

run: ## Run the API locally, loading .env when present
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/api

test: ## Run unit tests with the race detector
	go test -race -count=1 ./...

cover: ## Run tests and print the total coverage
	go test -race -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1

vet: ## Run go vet
	go vet ./...

fmt: ## Format the code
	go fmt ./...

lint: ## Run golangci-lint when installed, otherwise fall back to go vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found, falling back to go vet"; \
		go vet ./...; \
	fi

tidy: ## Tidy go.mod and go.sum
	go mod tidy

clean: ## Remove build and coverage artifacts
	rm -rf bin coverage.out coverage.html
