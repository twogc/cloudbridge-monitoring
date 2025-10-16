#!/bin/bash
# scripts/deploy-ha-monitoring.sh

set -e

echo "Развертывание CloudBridge Monitoring Stack (HA)"

# Функция для проверки существования файлов
check_files() {
    local dir=$1
    local files=("${@:2}")
    
    for file in "${files[@]}"; do
        if [ ! -f "$dir/$file" ]; then
            echo "ERROR: Файл не найден: $dir/$file"
            exit 1
        fi
    done
}

# Функция для проверки готовности подов
wait_for_pods() {
    local label=$1
    local timeout=${2:-300}
    
    echo "Ожидание готовности подов с лейблом: $label..."
    if ! kubectl wait --for=condition=ready pod -l "$label" -n cloudbridge-monitoring --timeout="${timeout}s"; then
        echo "ERROR: Таймаут ожидания готовности подов: $label"
        kubectl get pods -n cloudbridge-monitoring
        exit 1
    fi
}

# Функция для проверки здоровья сервисов
check_service_health() {
    local service=$1
    local port=$2
    local path=${3:-/}
    
    echo "Проверка здоровья сервиса: $service"
    kubectl run health-check --rm -i --restart=Never --image=curlimages/curl -- \
        curl -f "http://$service.cloudbridge-monitoring.svc.cluster.local:$port$path" || {
        echo "ERROR: Сервис $service недоступен"
        return 1
    }
}

# 1. Создание namespace
echo "Создание namespace..."
kubectl create namespace cloudbridge-monitoring --dry-run=client -o yaml | kubectl apply -f -

# 2. Развертывание RBAC
echo "Развертывание RBAC..."
kubectl apply -f security/rbac.yaml

# 3. Развертывание NetworkPolicies
echo "Развертывание NetworkPolicies..."
kubectl apply -f security/network-policies.yaml

# 4. Развертывание VictoriaMetrics
echo "Развертывание VictoriaMetrics..."
kubectl apply -f victoria-metrics/

# 5. Ожидание готовности VictoriaMetrics
wait_for_pods "app=victoria-metrics"

# 6. Развертывание HA Prometheus
echo "Развертывание HA Prometheus..."
kubectl apply -f prometheus/prometheus-ha.yaml

# 7. Ожидание готовности Prometheus
wait_for_pods "app=prometheus"

# 8. Развертывание SLO rules
echo "Развертывание SLO rules..."
kubectl apply -f prometheus/practical-slo-metrics.yaml
kubectl apply -f prometheus/slo-burn-rate-rules.yaml

# 9. Развертывание HA Grafana
echo "Развертывание HA Grafana..."
kubectl apply -f grafana/grafana-ha.yaml

# 10. Ожидание готовности Grafana
wait_for_pods "app=grafana"

# 11. Развертывание HA Alertmanager
echo "Развертывание HA Alertmanager..."
kubectl apply -f alertmanager/alertmanager-ha.yaml

# 12. Ожидание готовности Alertmanager
wait_for_pods "app=alertmanager"

# 13. Развертывание экспортеров
echo "Развертывание экспортеров..."
kubectl apply -f exporters/

# 14. Ожидание готовности экспортеров
wait_for_pods "app=blackbox-exporter"

# 15. Проверка здоровья сервисов
echo "Проверка здоровья сервисов..."
check_service_health "prometheus" "9090" "/-/healthy"
check_service_health "grafana" "3000" "/api/health"
check_service_health "alertmanager" "9093" "/-/healthy"
check_service_health "victoria-metrics" "8429" "/health"

# 16. Проверка готовности
echo "Проверка готовности HA Monitoring Stack..."
kubectl get pods -n cloudbridge-monitoring

# 17. Проверка сервисов
echo "Проверка сервисов..."
kubectl get svc -n cloudbridge-monitoring

# 18. Проверка ConfigMaps
echo "Проверка ConfigMaps..."
kubectl get configmaps -n cloudbridge-monitoring

# 19. Проверка PVC
echo "Проверка PVC..."
kubectl get pvc -n cloudbridge-monitoring

# 20. Проверка NetworkPolicies
echo "Проверка NetworkPolicies..."
kubectl get networkpolicies -n cloudbridge-monitoring

echo "SUCCESS: HA Monitoring Stack развернут успешно!"
echo ""
echo "Компоненты:"
echo "  - Prometheus HA: 2 реплики"
echo "  - Grafana HA: 2 реплики + PostgreSQL"
echo "  - Alertmanager HA: 2 реплики в кластере"
echo "  - VictoriaMetrics: долговременное хранение"
echo "  - Blackbox Exporter: synthetic monitoring"
echo "  - NetworkPolicies: безопасность"
echo "  - RBAC: минимальные права"
echo ""
echo "Доступные сервисы:"
echo "  - Prometheus: kubectl port-forward -n cloudbridge-monitoring svc/prometheus 9090:9090"
echo "  - Grafana: kubectl port-forward -n cloudbridge-monitoring svc/grafana 3000:3000"
echo "  - Alertmanager: kubectl port-forward -n cloudbridge-monitoring svc/alertmanager 9093:9093"
echo "  - VictoriaMetrics: kubectl port-forward -n cloudbridge-monitoring svc/victoria-metrics 8429:8429"
echo ""
echo "Учетные данные Grafana:"
echo "  - Username: admin"
echo "  - Password: admin123"
echo "  - OIDC: настроен для Zitadel"
echo ""
echo "SLO мониторинг:"
echo "  - Burn rate алерты: 5m/1h/6h окна"
echo "  - Error budget tracking"
echo "  - Dead man's switch"
echo "  - Data gap detection"
echo ""
echo "Безопасность:"
echo "  - NetworkPolicies: изолированные сети"
echo "  - RBAC: минимальные права"
echo "  - TLS: готов к настройке"
echo "  - Secrets: внешние секреты"
