# CloudBridge Monitoring - Быстрый старт тестирования

## Быстрый запуск

### 1. Установка зависимостей

```bash
cd exporters
make install-tools
```

Это установит:
- staticcheck
- golangci-lint
- gosec

### 2. Запуск всех тестов

```bash
make test-all
```

Или по отдельности:

```bash
# Unit тесты
make test

# Тесты с race detection
make test-race

# Тесты с покрытием
make test-coverage

# Интеграционные тесты
make test-integration

# Тесты производительности
make test-performance

# Тесты безопасности
make test-security
```

### 3. Проверка качества кода

```bash
# Форматирование
make fmt

# Линтеры
make lint

# go vet
make vet

# staticcheck
make staticcheck

# Анализ безопасности
make gosec
```

### 4. Сборка

```bash
# Сборка бинарных файлов
make build

# Сборка Docker образов
make build-docker
```

### 5. Просмотр покрытия

```bash
make test-coverage
# Откройте coverage.html в браузере
open coverage.html
```

## Быстрый цикл разработки

```bash
# Форматирование + проверка + тесты + сборка
make dev
```

## Подготовка к production

```bash
# Полная проверка готовности
make prod-ready
```

## CI/CD

Тесты запускаются автоматически при:
- Push в main/develop
- Pull Request

Проверьте статус в GitHub Actions.

## Структура тестов

```
exporters/
├── flow-telemetry/
│   ├── collector_test.go       # Тесты коллектора
│   ├── exporter_test.go        # Тесты экспортера
│   ├── main_test.go            # Интеграционные тесты
│   └── aggregator_test.go      # Тесты агрегатора
├── ddos-auto-mitigator/
│   └── main_test.go            # Тесты DDoS детектора
├── scrubbing-exporter/
│   └── main_test.go            # Тесты scrubbing экспортера
└── bgp-blackhole-exporter/
    └── main_test.go            # Тесты BGP blackhole экспортера
```

## Полезные команды

```bash
# Обновить зависимости
make update-deps

# Очистить артефакты сборки
make clean

# Запуск benchmark тестов
make dev-bench

# Профилирование
make dev-profile

# Запуск в Docker
make docker-test
```

## Troubleshooting

### Проблема: Тесты падают с "operation not permitted"

**Решение:** Запустите тесты вне sandbox:
```bash
go test -v ./...
```

### Проблема: Низкое покрытие тестами

**Решение:** Проверьте, какие функции не покрыты:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep -v "100.0%"
```

### Проблема: Медленные тесты

**Решение:** Запустите только быстрые тесты:
```bash
go test -short ./...
```

## Дополнительная информация

Подробная документация: [TESTING_IMPROVEMENTS.md](TESTING_IMPROVEMENTS.md)

## Контакты

При возникновении вопросов или проблем, пожалуйста, создайте issue в репозитории.
