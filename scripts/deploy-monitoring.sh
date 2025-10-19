#!/bin/bash
# scripts/deploy-monitoring.sh

set -e

echo "Развертывание CloudBridge Monitoring Stack"

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

# 2. Проверка файлов Prometheus
echo "Проверка файлов Prometheus..."
check_files "prometheus/monitoring" "prometheus-config.yaml" "prometheus-deployment.yaml"

# 3. Развертывание Prometheus
echo "Развертывание Prometheus..."
kubectl apply -f prometheus/monitoring/

# 4. Ожидание готовности Prometheus
wait_for_pods "app=prometheus"

# 5. Проверка файлов Grafana
echo "Проверка файлов Grafana..."
check_files "grafana" "grafana-deployment.yaml"
check_files "grafana/datasources" "prometheus.yml"

# 6. Развертывание Grafana
echo "Развертывание Grafana..."
kubectl apply -f grafana/

# 7. Ожидание готовности Grafana
wait_for_pods "app=grafana"

# 8. Проверка файлов Alertmanager
echo "Проверка файлов Alertmanager..."
check_files "alertmanager" "alertmanager-deployment.yaml"

# 9. Развертывание Alertmanager
echo "Развертывание Alertmanager..."
kubectl apply -f alertmanager/

# 10. Ожидание готовности Alertmanager
wait_for_pods "app=alertmanager"

# 11. Проверка здоровья сервисов
echo "Проверка здоровья сервисов..."
check_service_health "prometheus" "9090" "/-/healthy"
check_service_health "grafana" "3000" "/api/health"
check_service_health "alertmanager" "9093" "/-/healthy"

# 12. Проверка готовности
echo "Проверка готовности Monitoring Stack..."
kubectl get pods -n cloudbridge-monitoring

# 13. Проверка сервисов
echo "Проверка сервисов..."
kubectl get svc -n cloudbridge-monitoring

# 14. Проверка ConfigMaps
echo "Проверка ConfigMaps..."
kubectl get configmaps -n cloudbridge-monitoring

echo "SUCCESS: Monitoring Stack развернут успешно!"
echo ""
echo "Доступные сервисы:"
echo "  - Prometheus: kubectl port-forward -n cloudbridge-monitoring svc/prometheus 9090:9090"
echo "  - Grafana: kubectl port-forward -n cloudbridge-monitoring svc/grafana 3000:3000"
echo "  - Alertmanager: kubectl port-forward -n cloudbridge-monitoring svc/alertmanager 9093:9093"
echo ""
echo "Учетные данные Grafana:"
echo "  - Username: admin"
echo "  - Password: admin123"
echo ""
echo "Проверка метрик:"
echo "  - Prometheus: http://localhost:9090 (после port-forward)"
echo "  - Grafana: http://localhost:3000 (после port-forward)"
echo "  - Alertmanager: http://localhost:9093 (после port-forward)"










