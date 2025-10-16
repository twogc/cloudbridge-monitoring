# CloudBridge Monitoring and Metrics

Система мониторинга и метрик для CloudBridge Relay с SLO мониторингом, алертингом и визуализацией.

## Назначение

Этот репозиторий содержит компоненты мониторинга для CloudBridge Relay:

- **Prometheus** - Сбор и хранение метрик
- **Grafana** - Визуализация и дашборды
- **Alertmanager** - Управление алертами
- **SLO Monitoring** - Мониторинг Service Level Objectives
- **Logging** - Централизованное логирование

## Архитектура мониторинга

```mermaid
graph TB
    subgraph "Monitoring Stack"
        P[Prometheus<br/>monitor.2gc.ru<br/>Metrics Collection<br/>Time Series DB]
        G[Grafana<br/>grafana.2gc.ru<br/>Dashboards<br/>Visualization]
        A[Alertmanager<br/>alerts.2gc.ru<br/>Alert Management<br/>Notifications]
    end
    
    subgraph "Data Sources"
        R1[Moscow PoP<br/>Metrics Export]
        R2[Frankfurt PoP<br/>Metrics Export]
        R3[Amsterdam PoP<br/>Metrics Export]
        CP[Control Plane<br/>Metrics Export]
        AI[AI Service<br/>Metrics Export]
    end
    
    subgraph "SLO Targets"
        S1[RTT p95<br/>< 50ms]
        S2[Handover p95<br/>< 300ms]
        S3[P2P Percentage<br/>≥ 60%]
        S4[Error Budget<br/>< 5%]
    end
    
    R1 --> P
    R2 --> P
    R3 --> P
    CP --> P
    AI --> P
    
    P --> G
    P --> A
    
    G --> S1
    G --> S2
    G --> S3
    G --> S4
    
    classDef monitoring fill:#e3f2fd,stroke:#1976d2,stroke-width:2px
    classDef datasource fill:#f3e5f5,stroke:#7b1fa2,stroke-width:2px
    classDef slo fill:#e8f5e8,stroke:#388e3c,stroke-width:2px
    
    class P,G,A monitoring
    class R1,R2,R3,CP,AI datasource
    class S1,S2,S3,S4 slo
```

## Компоненты

### **Prometheus**
- **Назначение**: Сбор и хранение метрик
- **Функции**: Time series database, metrics collection, querying
- **Конфигурация**: `prometheus/`

### **Grafana**
- **Назначение**: Визуализация метрик и дашборды
- **Функции**: Dashboards, alerting, data sources
- **Конфигурация**: `grafana/`

### **Alertmanager**
- **Назначение**: Управление алертами и уведомлениями
- **Функции**: Alert routing, notification channels
- **Конфигурация**: `alertmanager/`

## Быстрый старт

### **Развертывание мониторинга**

```bash
# 1. Создание namespace
kubectl create namespace cloudbridge-monitoring

# 2. Развертывание Prometheus
kubectl apply -f prometheus/

# 3. Развертывание Grafana
kubectl apply -f grafana/

# 4. Развертывание Alertmanager
kubectl apply -f alertmanager/

# 5. Проверка готовности
kubectl get pods -n cloudbridge-monitoring
```

### **Доступ к интерфейсам**

```bash
# Prometheus
kubectl port-forward -n cloudbridge-monitoring svc/prometheus 9090:9090

# Grafana
kubectl port-forward -n cloudbridge-monitoring svc/grafana 3000:3000

# Alertmanager
kubectl port-forward -n cloudbridge-monitoring svc/alertmanager 9093:9093
```

## Конфигурация

### **Prometheus конфигурация**

```yaml
# prometheus/prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

rule_files:
  - "slo-rules.yml"
  - "alert-rules.yml"

scrape_configs:
  - job_name: 'relay-metrics'
    static_configs:
      - targets: ['relay-mow:8080', 'relay-fra:8080', 'relay-ams:8080']
    metrics_path: '/metrics'
    scrape_interval: 5s
  
  - job_name: 'control-plane'
    static_configs:
      - targets: ['zitadel:8080', 'cadence:8080', 'postgresql:5432']
    metrics_path: '/metrics'
  
  - job_name: 'ai-service'
    static_configs:
      - targets: ['ai-service:8083']
    metrics_path: '/metrics'
```

### **Grafana дашборды**

```json
{
  "dashboard": {
    "title": "CloudBridge SLO Dashboard",
    "panels": [
      {
        "title": "RTT p95",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, relay_rtt_seconds)",
            "legendFormat": "RTT p95"
          }
        ],
        "thresholds": [
          {
            "value": 0.05,
            "color": "red",
            "op": "gt"
          }
        ]
      },
      {
        "title": "Handover Duration p95",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, relay_handover_duration_seconds)",
            "legendFormat": "Handover p95"
          }
        ],
        "thresholds": [
          {
            "value": 0.3,
            "color": "red",
            "op": "gt"
          }
        ]
      },
      {
        "title": "P2P Percentage",
        "type": "stat",
        "targets": [
          {
            "expr": "relay_p2p_percentage",
            "legendFormat": "P2P %"
          }
        ],
        "thresholds": [
          {
            "value": 60,
            "color": "red",
            "op": "lt"
          }
        ]
      }
    ]
  }
}
```

### **SLO Rules**

```yaml
# prometheus/slo-rules.yml
groups:
- name: slo.rules
  rules:
  - alert: HighRTT
    expr: histogram_quantile(0.95, relay_rtt_seconds) > 0.05
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High RTT detected"
      description: "RTT p95 is {{ $value }}s, threshold is 0.05s"
  
  - alert: SlowHandover
    expr: histogram_quantile(0.95, relay_handover_duration_seconds) > 0.3
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "Slow handover detected"
      description: "Handover p95 is {{ $value }}s, threshold is 0.3s"
  
  - alert: LowP2PPercentage
    expr: relay_p2p_percentage < 60
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "Low P2P percentage"
      description: "P2P percentage is {{ $value }}%, threshold is 60%"
```

## Ключевые метрики

### **Relay метрики**

| Метрика | Описание | SLO Target |
|---------|----------|------------|
| `relay_rtt_seconds` | RTT между PoP | p95 < 50ms |
| `relay_handover_duration_seconds` | Время handover | p95 < 300ms |
| `relay_p2p_percentage` | % P2P соединений | ≥ 60% |
| `relay_error_rate` | Частота ошибок | < 1% |
| `relay_throughput_bytes` | Пропускная способность | > 1Gbps |

### **Control Plane метрики**

| Метрика | Описание | SLO Target |
|---------|----------|------------|
| `zitadel_health` | Состояние Zitadel | = 1 |
| `cadence_workflows_active` | Активные workflows | < 1000 |
| `postgresql_connections` | Подключения к БД | < 100 |
| `redis_memory_usage` | Использование памяти Redis | < 80% |

### **AI Service метрики**

| Метрика | Описание | SLO Target |
|---------|----------|------------|
| `ai_routing_decisions_total` | Решения по маршрутизации | > 0 |
| `ai_model_accuracy` | Точность AI модели | > 90% |
| `ai_prediction_latency` | Задержка предсказаний | < 100ms |

## Алертинг

### **Alert Channels**

```yaml
# alertmanager/alertmanager.yml
global:
  smtp_smarthost: 'smtp.gmail.com:587'
  smtp_from: 'alerts@2gc.ru'

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'web.hook'

receivers:
- name: 'web.hook'
  webhook_configs:
  - url: 'http://webhook:5001/'
  
- name: 'email'
  email_configs:
  - to: 'admin@2gc.ru'
    subject: 'CloudBridge Alert: {{ .GroupLabels.alertname }}'
    body: |
      {{ range .Alerts }}
      Alert: {{ .Annotations.summary }}
      Description: {{ .Annotations.description }}
      {{ end }}
```

### **Alert Rules**

```yaml
# prometheus/alert-rules.yml
groups:
- name: cloudbridge.alerts
  rules:
  - alert: RelayDown
    expr: up{job="relay-metrics"} == 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "Relay server is down"
      description: "Relay server {{ $labels.instance }} is down"
  
  - alert: HighErrorRate
    expr: rate(relay_errors_total[5m]) > 0.01
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High error rate detected"
      description: "Error rate is {{ $value }} errors/sec"
```

## SLO Monitoring

### **SLO Configuration**

```yaml
# slo/slo-config.yaml
slos:
  - name: "RTT SLO"
    target: 0.95
    window: "30d"
    metric: "histogram_quantile(0.95, relay_rtt_seconds)"
    threshold: 0.05
  
  - name: "Handover SLO"
    target: 0.99
    window: "7d"
    metric: "histogram_quantile(0.95, relay_handover_duration_seconds)"
    threshold: 0.3
  
  - name: "P2P SLO"
    target: 0.90
    window: "30d"
    metric: "relay_p2p_percentage"
    threshold: 60
```

### **Error Budget**

```yaml
# slo/error-budget.yaml
error_budget:
  total_budget: 0.05  # 5% error budget
  consumed_budget: 0.02  # 2% consumed
  remaining_budget: 0.03  # 3% remaining
  burn_rate: 0.001  # 0.1% per hour
```

## Тестирование

### **Monitoring тесты**

```bash
# Тестирование метрик
./scripts/test-metrics.sh

# Тестирование алертов
./scripts/test-alerts.sh

# Тестирование SLO
./scripts/test-slo.sh
```

### **Load тесты**

```bash
# Нагрузочное тестирование мониторинга
./scripts/load-test-monitoring.sh
```

## Масштабирование

### **Горизонтальное масштабирование**

```bash
# Масштабирование Prometheus
kubectl scale deployment prometheus -n cloudbridge-monitoring --replicas=3

# Масштабирование Grafana
kubectl scale deployment grafana -n cloudbridge-monitoring --replicas=2
```

### **Вертикальное масштабирование**

```yaml
# Увеличение ресурсов
resources:
  requests:
    cpu: "2"
    memory: "4Gi"
  limits:
    cpu: "4"
    memory: "8Gi"
```

## Обновления

### **Rolling update**

```bash
# Обновление Prometheus
kubectl set image deployment/prometheus prometheus=prom/prometheus:v2.45.0 -n cloudbridge-monitoring

# Обновление Grafana
kubectl set image deployment/grafana grafana=grafana/grafana:10.0.0 -n cloudbridge-monitoring
```

### **Rollback**

```bash
# Откат к предыдущей версии
kubectl rollout undo deployment/prometheus -n cloudbridge-monitoring
```

## Документация

- [Prometheus Configuration](docs/prometheus-config.md)
- [Grafana Dashboards](docs/grafana-dashboards.md)
- [Alerting Rules](docs/alerting-rules.md)
- [SLO Monitoring](docs/slo-monitoring.md)
- [Troubleshooting](docs/troubleshooting.md)

## Участие в разработке

1. Fork репозитория
2. Создайте feature branch
3. Внесите изменения
4. Создайте Pull Request

## Лицензия

MIT License - см. [LICENSE](LICENSE) файл.

## Поддержка

- **Issues**: [GitHub Issues](https://github.com/2gc-dev/cloudbridge-monitoring/issues)
- **Discussions**: [GitHub Discussions](https://github.com/2gc-dev/cloudbridge-monitoring/discussions)
- **Documentation**: [Wiki](https://github.com/2gc-dev/cloudbridge-monitoring/wiki)




