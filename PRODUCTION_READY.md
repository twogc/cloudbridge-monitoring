# CloudBridge Monitoring - Production Ready

## Обзор

Система мониторинга CloudBridge теперь полностью готова к production использованию с поддержкой высокой доступности, безопасности и продвинутых функций.

## Архитектура

### High Availability
- **Prometheus HA**: 2 реплики с remote write в VictoriaMetrics
- **Grafana HA**: 2 реплики с внешней PostgreSQL БД
- **Alertmanager HA**: 2 реплики в кластере с shared state
- **VictoriaMetrics**: долговременное хранение (180 дней)

### Безопасность
- **NetworkPolicies**: изолированные сети между компонентами
- **RBAC**: минимальные права доступа
- **OIDC**: интеграция с Zitadel для аутентификации
- **Secrets**: внешние секреты для чувствительных данных

### Мониторинг
- **SLO Burn Rate**: multi-window алерты (5m/1h/6h)
- **Error Budget**: tracking и алерты
- **Dead Man's Switch**: контроль работы алертинга
- **Data Gap Detection**: обнаружение пропусков данных

## Компоненты

### 1. Prometheus HA
```yaml
# 2 реплики с remote write
replicas: 2
remote_write:
  - url: "http://victoria-metrics-vmagent:8429/api/v1/write"
```

### 2. Grafana HA
```yaml
# 2 реплики + PostgreSQL
replicas: 2
database: postgres
oidc: zitadel
```

### 3. Alertmanager HA
```yaml
# 2 реплики в кластере
replicas: 2
cluster:
  listen-address: ":9094"
```

### 4. VictoriaMetrics
```yaml
# Долговременное хранение
retention: 180d
storage: 100Gi
```

## Развертывание

### Быстрый старт
```bash
# HA развертывание
./scripts/deploy-ha-monitoring.sh

# Проверка
./scripts/test-monitoring.sh
```

### Компоненты
```bash
# RBAC и безопасность
kubectl apply -f security/

# Prometheus HA
kubectl apply -f prometheus/prometheus-ha.yaml

# Grafana HA
kubectl apply -f grafana/grafana-ha.yaml

# Alertmanager HA
kubectl apply -f alertmanager/alertmanager-ha.yaml

# VictoriaMetrics
kubectl apply -f victoria-metrics/

# Экспортеры
kubectl apply -f exporters/
```

## SLO Monitoring

### Burn Rate Alerts
- **5m window**: warning при 95% SLO
- **1h window**: critical при 99% SLO  
- **6h window**: critical при 99.9% SLO

### Error Budget
- **30d window**: tracking error budget
- **Alerts**: при исчерпании бюджета

### Multi-window Detection
- **RTT SLO**: p95 < 50ms
- **Handover SLO**: p95 < 300ms
- **P2P SLO**: > 60% P2P соединений

## Безопасность

### NetworkPolicies
```yaml
# Изоляция компонентов
prometheus: только от grafana и экспортеров
grafana: доступ к prometheus и alertmanager
alertmanager: только от prometheus
```

### RBAC
```yaml
# Минимальные права
prometheus: read-only k8s API
grafana: read-only для дашбордов
alertmanager: минимальные права
```

### OIDC Integration
```yaml
# Zitadel OIDC
client_id: grafana
scopes: openid profile email
```

## Мониторинг мониторинга

### Обязательные алерты
- **Prometheus TSDB**: corruption, high series count
- **Alertmanager**: failed notifications
- **Grafana**: health checks
- **Data gaps**: отсутствие данных
- **Dead man's switch**: контроль алертинга

### Synthetic Monitoring
- **Blackbox Exporter**: HTTP/HTTPS/TCP/ICMP проверки
- **Inter-PoP RTT**: активные проверки
- **Service Health**: health endpoints

## CI/CD

### Автоматизация
- **Linting**: yamllint, promtool, kubeconform
- **Testing**: kind cluster, e2e тесты
- **Security**: Trivy vulnerability scanning
- **Validation**: Kubernetes manifests

### GitHub Actions
```yaml
# Автоматические проверки
- lint: YAML, Prometheus rules, Grafana dashboards
- test: kind cluster, monitoring tests
- security: Trivy scanning
- build: custom exporters
```

## Операционные процедуры

### Backup
- **Grafana**: PostgreSQL дамп
- **Alertmanager**: silences в PVC
- **VictoriaMetrics**: объектное хранилище
- **ConfigMaps**: Git репозиторий

### Recovery
- **Prometheus**: восстановление из VictoriaMetrics
- **Grafana**: восстановление из PostgreSQL
- **Alertmanager**: восстановление silences
- **Config**: восстановление из Git

### Scaling
- **Prometheus**: горизонтальное масштабирование
- **Grafana**: load balancer
- **Alertmanager**: кластер автоматически
- **VictoriaMetrics**: вертикальное масштабирование

## Метрики и алерты

### Ключевые метрики
- **RTT**: histogram_quantile(0.95, relay_rtt_seconds)
- **Jitter**: histogram_quantile(0.95, relay_jitter_seconds)
- **Loss**: rate(relay_packets_lost_total) / rate(relay_packets_total)
- **Handover**: histogram_quantile(0.95, relay_handover_duration_seconds)
- **P2P**: rate(relay_p2p_connections_total) / rate(relay_total_connections_total)

### Алерты
- **Critical**: немедленная доставка
- **Warning**: группировка 5 минут
- **SLO**: специальная обработка
- **Dead man's switch**: контроль системы

## Дашборды

### Основные дашборды
- **CloudBridge Overview**: общий обзор системы
- **Network Performance**: RTT, jitter, loss
- **SLO Dashboard**: burn rate, error budget
- **Alert Dashboard**: активные алерты
- **Infrastructure**: ресурсы кластера

### SLO дашборды
- **Home Network**: домашние сети
- **Mobile Network**: мобильные сети
- **Streaming**: стриминг трафик
- **Gaming**: игровой трафик
- **Prime Time**: пиковые нагрузки

## Troubleshooting

### Частые проблемы
1. **Prometheus OOM**: увеличить memory limits
2. **Grafana медленно**: проверить PostgreSQL
3. **Алерты не доставляются**: проверить SMTP/Slack
4. **Data gaps**: проверить экспортеры

### Команды диагностики
```bash
# Проверка подов
kubectl get pods -n cloudbridge-monitoring

# Проверка логов
kubectl logs -n cloudbridge-monitoring -l app=prometheus

# Проверка метрик
curl http://prometheus:9090/api/v1/query?query=up

# Проверка алертов
curl http://alertmanager:9093/api/v1/alerts
```

## Стоимость

### Ресурсы
- **CPU**: 4-8 cores
- **Memory**: 16-32 GB
- **Storage**: 200-500 GB
- **Network**: 1-10 Gbps

### Оптимизация
- **Retention**: 15d локально, 180d в VictoriaMetrics
- **Scrape intervals**: 15s-60s в зависимости от метрики
- **Cardinality**: relabel_configs для снижения
- **Compression**: включена в VictoriaMetrics

## Roadmap

### Краткосрочно (1-2 месяца)
- [ ] Настройка TLS сертификатов
- [ ] External Secrets Operator
- [ ] Custom CloudBridge exporter
- [ ] Runbooks документация

### Среднесрочно (3-6 месяцев)
- [ ] Machine learning алерты
- [ ] Automated remediation
- [ ] Cost optimization
- [ ] Multi-cluster monitoring

### Долгосрочно (6+ месяцев)
- [ ] Predictive scaling
- [ ] Advanced analytics
- [ ] Integration с ITSM
- [ ] Compliance reporting

## Заключение

Система мониторинга CloudBridge теперь полностью готова к production использованию с поддержкой:

- ✅ **High Availability**: 2+ реплики всех компонентов
- ✅ **Security**: NetworkPolicies, RBAC, OIDC
- ✅ **SLO Monitoring**: burn rate, error budget
- ✅ **Synthetic Monitoring**: blackbox exporter
- ✅ **CI/CD**: автоматизированные тесты
- ✅ **Operational Excellence**: backup, recovery, scaling

Система обеспечивает надежный мониторинг CloudBridge платформы с возможностью масштабирования и развития.
