# CloudBridge Monitoring - Makefile
# Provides convenient commands for testing, building, and running the monitoring system

.PHONY: help test test-race test-coverage test-integration test-performance test-security test-all build clean lint fmt vet

# Default target
help:
	@echo "CloudBridge Monitoring - Available Commands:"
	@echo ""
	@echo "Testing:"
	@echo "  test              - Run all tests"
	@echo "  test-race         - Run tests with race detection"
	@echo "  test-coverage     - Run tests with coverage report"
	@echo "  test-integration  - Run integration tests"
	@echo "  test-performance  - Run performance tests"
	@echo "  test-security     - Run security tests"
	@echo "  test-all          - Run all test types"
	@echo ""
	@echo "Building:"
	@echo "  build             - Build all binaries"
	@echo "  build-docker      - Build Docker images"
	@echo "  clean             - Clean build artifacts"
	@echo ""
	@echo "Code Quality:"
	@echo "  lint              - Run linters"
	@echo "  fmt               - Format code"
	@echo "  vet               - Run go vet"
	@echo "  staticcheck       - Run staticcheck"
	@echo "  gosec             - Run security analysis"
	@echo ""
	@echo "Development:"
	@echo "  dev-setup         - Setup development environment"
	@echo "  dev-test          - Run development tests"
	@echo "  dev-bench         - Run benchmarks"
	@echo "  dev-profile       - Run profiling"

# Testing commands
test:
	@echo "Running tests..."
	go test -v ./...

test-race:
	@echo "Running tests with race detection..."
	go test -race -v ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-integration:
	@echo "Running integration tests..."
	go test -tags=integration -v ./...

test-performance:
	@echo "Running performance tests..."
	go test -tags=performance -v ./...

test-security:
	@echo "Running security tests..."
	go test -tags=security -v ./...

test-all: test-race test-coverage test-integration test-performance test-security
	@echo "All tests completed"

# Building commands
build:
	@echo "Building binaries..."
	@mkdir -p bin
	go build -o bin/scrubbing-exporter ./scrubbing-exporter
	go build -o bin/bgp-blackhole-exporter ./bgp-blackhole-exporter
	go build -o bin/flow-telemetry ./flow-telemetry
	go build -o bin/ddos-auto-mitigator ./ddos-auto-mitigator
	@echo "Binaries built in bin/ directory"

build-docker:
	@echo "Building Docker images..."
	docker build -t cloudbridge/scrubbing-exporter:latest ./scrubbing-exporter
	docker build -t cloudbridge/bgp-blackhole-exporter:latest ./bgp-blackhole-exporter
	docker build -t cloudbridge/flow-telemetry:latest ./flow-telemetry
	docker build -t cloudbridge/ddos-auto-mitigator:latest ./ddos-auto-mitigator
	@echo "Docker images built"

clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f *.log
	@echo "Clean completed"

# Code quality commands
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

fmt:
	@echo "Formatting code..."
	go fmt ./...

vet:
	@echo "Running go vet..."
	go vet ./...

staticcheck:
	@echo "Running staticcheck..."
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed, skipping..."; \
	fi

gosec:
	@echo "Running security analysis..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed, skipping..."; \
	fi

# Development commands
dev-setup:
	@echo "Setting up development environment..."
	go mod download
	go mod verify
	@echo "Development environment ready"

dev-test: test-race test-coverage
	@echo "Development tests completed"

dev-bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

dev-profile:
	@echo "Running profiling..."
	go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
	@echo "Profiles generated: cpu.prof, mem.prof"

# CI/CD commands
ci-test: test-race test-coverage
	@echo "CI tests completed"

ci-build: build
	@echo "CI build completed"

ci-quality: fmt vet staticcheck gosec
	@echo "CI quality checks completed"

# Docker commands
docker-build:
	@echo "Building Docker images..."
	docker build -t cloudbridge/scrubbing-exporter:latest ./scrubbing-exporter
	docker build -t cloudbridge/bgp-blackhole-exporter:latest ./bgp-blackhole-exporter
	docker build -t cloudbridge/flow-telemetry:latest ./flow-telemetry
	docker build -t cloudbridge/ddos-auto-mitigator:latest ./ddos-auto-mitigator

docker-test:
	@echo "Running tests in Docker..."
	docker run --rm -v $(PWD):/app -w /app golang:1.23 go test -v ./...

docker-clean:
	@echo "Cleaning Docker images..."
	docker rmi cloudbridge/scrubbing-exporter:latest || true
	docker rmi cloudbridge/bgp-blackhole-exporter:latest || true
	docker rmi cloudbridge/flow-telemetry:latest || true
	docker rmi cloudbridge/ddos-auto-mitigator:latest || true

# Monitoring commands
monitor-start:
	@echo "Starting monitoring services..."
	@echo "This would start Prometheus, Grafana, etc."

monitor-stop:
	@echo "Stopping monitoring services..."

monitor-status:
	@echo "Checking monitoring services status..."

# Utility commands
install-tools:
	@echo "Installing development tools..."
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	@echo "Tools installed"

update-deps:
	@echo "Updating dependencies..."
	go get -u ./...
	go mod tidy
	@echo "Dependencies updated"

# Performance testing
perf-test:
	@echo "Running performance tests..."
	go test -bench=. -benchmem -benchtime=30s ./...

perf-profile:
	@echo "Running performance profiling..."
	go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof ./...

# Security testing
security-scan:
	@echo "Running security scan..."
	gosec ./...

security-test:
	@echo "Running security tests..."
	go test -tags=security -v ./...

# Integration testing
integration-test:
	@echo "Running integration tests..."
	go test -tags=integration -v ./...

# Full pipeline
pipeline: clean install-tools fmt vet staticcheck gosec test-all build
	@echo "Full pipeline completed successfully"

# Quick development cycle
dev: fmt vet test build
	@echo "Development cycle completed"

# Production readiness
prod-ready: clean fmt vet staticcheck gosec test-race test-coverage build
	@echo "Production readiness checks completed"