# QUIC Probe Exporter

QUIC probe exporter для мониторинга CloudBridge Relay с поддержкой QUIC протокола.

## Возможности

- **QUIC Handshake Monitoring**: измерение времени установления соединения
- **0-RTT Support**: мониторинг успешности 0-RTT соединений
- **RTT Measurement**: измерение round-trip time
- **Loss Ratio**: отслеживание потери пакетов
- **Multi-PoP Support**: мониторинг между разными PoP
- **Prometheus Metrics**: экспорт метрик в формате Prometheus

## Метрики

### QUIC Handshake
- `quic_handshake_duration_seconds` - время установления QUIC соединения
- `quic_handshake_success_total` - количество успешных handshake
- `quic_handshake_failures_total` - количество неудачных handshake

### QUIC Performance
- `quic_zero_rtt_success_total` - количество успешных 0-RTT соединений
- `quic_loss_ratio` - коэффициент потери пакетов
- `quic_rtt_seconds` - round-trip time

## Использование

### Запуск

```bash
# Базовый запуск
./quic-probe -targets=moscow.2gc.ru:443,frankfurt.2gc.ru:443 -interval=30s -port=8080

# С дополнительными параметрами
./quic-probe \
  -targets=moscow.2gc.ru:443,frankfurt.2gc.ru:443,amsterdam.2gc.ru:443 \
  -interval=30s \
  -port=8080
```

### Параметры

- `-targets`: список QUIC целей (по умолчанию: moscow.2gc.ru:443,frankfurt.2gc.ru:443,amsterdam.2gc.ru:443)
- `-interval`: интервал проверок (по умолчанию: 30s)
- `-port`: HTTP порт для метрик (по умолчанию: 8080)

### Docker

```bash
# Сборка образа
docker build -f Dockerfile.quic-probe -t cloudbridge/quic-probe:latest .

# Запуск контейнера
docker run -p 8080:8080 cloudbridge/quic-probe:latest \
  -targets=moscow.2gc.ru:443,frankfurt.2gc.ru:443 \
  -interval=30s \
  -port=8080
```

### Kubernetes

```bash
# Развертывание в Kubernetes
kubectl apply -f quic-probe.yaml

# Проверка статуса
kubectl get pods -l app=quic-probe -n cloudbridge-monitoring
```

## Разработка

### Требования

- Go 1.25+
- Docker (для сборки образа)
- Make (для удобства)

### Установка зависимостей

```bash
# Установка инструментов разработки
make install-tools

# Обновление зависимостей
make deps
```

### Сборка

```bash
# Сборка бинарника
make build

# Сборка для всех платформ
make build-all

# Сборка Docker образа
make docker-build
```

### Тестирование

```bash
# Запуск тестов
make test

# Тесты с race detection
make test-race

# Тесты с покрытием
make test-coverage

# Бенчмарки
make benchmark
```

### Линтинг и безопасность

```bash
# Линтинг
make lint

# Проверки безопасности
make security

# Все проверки для CI
make ci
```

### Запуск в режиме разработки

```bash
# Запуск бинарника
make run

# Запуск в режиме разработки
make run-dev

# Запуск Docker контейнера
make docker-run
```

## Архитектура

### Компоненты

1. **QUIC Probe**: основной компонент для мониторинга QUIC соединений
2. **Prometheus Metrics**: экспорт метрик в формате Prometheus
3. **HTTP Server**: сервер для предоставления метрик
4. **Health Checks**: проверки здоровья приложения

### Метрики

```go
// QUIC Handshake Duration
quicHandshakeDuration = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "quic_handshake_duration_seconds",
        Help: "Duration of QUIC handshake",
    },
    []string{"target", "pop"},
)

// QUIC Handshake Success
quicHandshakeSuccess = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "quic_handshake_success_total",
        Help: "Total successful QUIC handshakes",
    },
    []string{"target", "pop"},
)

// QUIC Handshake Failures
quicHandshakeFailures = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "quic_handshake_failures_total",
        Help: "Total failed QUIC handshakes",
    },
    []string{"target", "pop", "error_type"},
)
```

## Мониторинг

### Health Checks

```bash
# Проверка здоровья
curl http://localhost:8080/health

# Получение метрик
curl http://localhost:8080/metrics
```

### Prometheus Configuration

```yaml
scrape_configs:
- job_name: 'quic-probe'
  static_configs:
    - targets: ['quic-probe.cloudbridge-monitoring.svc.cluster.local:8080']
  scrape_interval: 30s
  metrics_path: /metrics
```

### Grafana Dashboard

Создайте дашборд для мониторинга QUIC метрик:

```json
{
  "dashboard": {
    "title": "QUIC Probe Metrics",
    "panels": [
      {
        "title": "QUIC Handshake Duration",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, quic_handshake_duration_seconds_bucket)"
          }
        ]
      },
      {
        "title": "QUIC Handshake Success Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(quic_handshake_success_total[5m]) / (rate(quic_handshake_success_total[5m]) + rate(quic_handshake_failures_total[5m]))"
          }
        ]
      }
    ]
  }
}
```

## Troubleshooting

### Частые проблемы

1. **QUIC Handshake Failed**: проверьте доступность целевых серверов
2. **High RTT**: проверьте сетевую задержку между PoP
3. **High Loss Ratio**: проверьте качество сети

### Логи

```bash
# Просмотр логов
kubectl logs -l app=quic-probe -n cloudbridge-monitoring

# Просмотр логов в реальном времени
kubectl logs -f -l app=quic-probe -n cloudbridge-monitoring
```

### Отладка

```bash
# Запуск с отладочными логами
./quic-probe -targets=localhost:443 -interval=10s -port=8080 -log-level=debug

# Проверка метрик
curl http://localhost:8080/metrics | grep quic_
```

## Лицензия

MIT License
