# Makefile для QUIC Probe

.PHONY: help build test clean run docker-build docker-run

# Переменные
BINARY_NAME=quic-probe
DOCKER_IMAGE=cloudbridge/quic-probe
DOCKER_TAG=latest
GO_VERSION=1.25.3

help: ## Показать справку
	@echo "Доступные команды:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Собрать бинарник
	@echo "Building $(BINARY_NAME)..."
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o $(BINARY_NAME) .

build-all: ## Собрать для всех платформ
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINARY_NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o $(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BINARY_NAME)-windows-amd64.exe .

test: ## Запустить тесты
	@echo "Running tests..."
	go test -v ./...

test-race: ## Запустить тесты с race detection
	@echo "Running tests with race detection..."
	go test -race -v ./...

test-coverage: ## Запустить тесты с покрытием
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

benchmark: ## Запустить бенчмарки
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

lint: ## Запустить линтеры
	@echo "Running linters..."
	golangci-lint run

security: ## Запустить проверки безопасности
	@echo "Running security checks..."
	govulncheck ./...
	gosec ./...

clean: ## Очистить артефакты
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-*
	rm -f coverage.out coverage.html
	docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) 2>/dev/null || true

run: build ## Собрать и запустить
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME) -targets=localhost:443,example.com:443 -interval=30s -port=8080

run-dev: ## Запустить в режиме разработки
	@echo "Running in development mode..."
	go run . -targets=localhost:443,example.com:443 -interval=30s -port=8080

docker-build: ## Собрать Docker образ
	@echo "Building Docker image..."
	docker build -f Dockerfile.quic-probe -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run: docker-build ## Собрать и запустить Docker образ
	@echo "Running Docker container..."
	docker run --rm -p 8080:8080 $(DOCKER_IMAGE):$(DOCKER_TAG) -targets=localhost:443,example.com:443 -interval=30s -port=8080

docker-test: docker-build ## Тестировать Docker образ
	@echo "Testing Docker image..."
	docker run --rm $(DOCKER_IMAGE):$(DOCKER_TAG) --help

deps: ## Обновить зависимости
	@echo "Updating dependencies..."
	go get -u=patch ./...
	go mod tidy

deps-check: ## Проверить зависимости
	@echo "Checking dependencies..."
	go mod verify
	go mod download

fmt: ## Форматировать код
	@echo "Formatting code..."
	go fmt ./...

vet: ## Запустить go vet
	@echo "Running go vet..."
	go vet ./...

install-tools: ## Установить инструменты разработки
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	go install honnef.co/go/tools/cmd/staticcheck@latest

ci: deps-check fmt vet test-race security ## Запустить все проверки для CI
	@echo "All CI checks passed!"

# Установка инструментов по умолчанию
.DEFAULT_GOAL := help
