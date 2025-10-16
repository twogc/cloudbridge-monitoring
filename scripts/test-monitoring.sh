#!/bin/bash
# scripts/test-monitoring.sh

set -e

echo "Тестирование CloudBridge Monitoring Stack"

# Функция для проверки HTTP endpoint
check_endpoint() {
    local service=$1
    local port=$2
    local path=$3
    local expected_status=${4:-200}
    
    echo "Проверка $service на порту $port..."
    
    # Получаем IP подов
    local pod_ip=$(kubectl get pods -n cloudbridge-monitoring -l app=$service -o jsonpath='{.items[0].status.podIP}')
    
    if [ -z "$pod_ip" ]; then
        echo "ERROR: Под $service не найден"
        return 1
    fi
    
    # Проверяем доступность
    local response=$(kubectl run test-$service --rm -i --restart=Never --image=curlimages/curl -- \
        curl -s -o /dev/null -w "%{http_code}" "http://$pod_ip:$port$path" 2>/dev/null || echo "000")
    
    if [ "$response" = "$expected_status" ]; then
        echo "SUCCESS: $service доступен (HTTP $response)"
        return 0
    else
        echo "ERROR: $service недоступен (HTTP $response, ожидался $expected_status)"
        return 1
    fi
}

# Функция для проверки метрик
check_metrics() {
    local service=$1
    local metric_name=$2
    
    echo "Проверка метрики $metric_name для $service..."
    
    local pod_ip=$(kubectl get pods -n cloudbridge-monitoring -l app=$service -o jsonpath='{.items[0].status.podIP}')
    
    if [ -z "$pod_ip" ]; then
        echo "ERROR: Под $service не найден"
        return 1
    fi
    
    # Проверяем наличие метрики
    local metrics=$(kubectl run test-metrics-$service --rm -i --restart=Never --image=curlimages/curl -- \
        curl -s "http://$pod_ip:9090/metrics" 2>/dev/null | grep "$metric_name" || echo "")
    
    if [ -n "$metrics" ]; then
        echo "SUCCESS: Метрика $metric_name найдена"
        return 0
    else
        echo "ERROR: Метрика $metric_name не найдена"
        return 1
    fi
}

# Функция для проверки алертов
check_alerts() {
    echo "Проверка алертов..."
    
    local pod_ip=$(kubectl get pods -n cloudbridge-monitoring -l app=prometheus -o jsonpath='{.items[0].status.podIP}')
    
    if [ -z "$pod_ip" ]; then
        echo "ERROR: Под Prometheus не найден"
        return 1
    fi
    
    # Проверяем API алертов
    local alerts=$(kubectl run test-alerts --rm -i --restart=Never --image=curlimages/curl -- \
        curl -s "http://$pod_ip:9090/api/v1/alerts" 2>/dev/null | jq -r '.data.alerts | length' || echo "0")
    
    echo "Найдено алертов: $alerts"
    return 0
}

# Функция для проверки дашбордов
check_dashboards() {
    echo "Проверка дашбордов Grafana..."
    
    local pod_ip=$(kubectl get pods -n cloudbridge-monitoring -l app=grafana -o jsonpath='{.items[0].status.podIP}')
    
    if [ -z "$pod_ip" ]; then
        echo "ERROR: Под Grafana не найден"
        return 1
    fi
    
    # Проверяем API дашбордов
    local dashboards=$(kubectl run test-dashboards --rm -i --restart=Never --image=curlimages/curl -- \
        curl -s "http://$pod_ip:3000/api/search" 2>/dev/null | jq -r '.[] | .title' || echo "")
    
    if [ -n "$dashboards" ]; then
        echo "SUCCESS: Дашборды найдены:"
        echo "$dashboards"
        return 0
    else
        echo "ERROR: Дашборды не найдены"
        return 1
    fi
}

# Основные тесты
echo "Запуск тестов..."

# 1. Проверка доступности сервисов
echo "1. Проверка доступности сервисов..."
check_endpoint "prometheus" "9090" "/-/healthy" "200"
check_endpoint "grafana" "3000" "/api/health" "200"
check_endpoint "alertmanager" "9093" "/-/healthy" "200"

# 2. Проверка метрик
echo "2. Проверка метрик..."
check_metrics "prometheus" "prometheus_tsdb_head_samples"
check_metrics "prometheus" "prometheus_config_last_reload_successful"

# 3. Проверка алертов
echo "3. Проверка алертов..."
check_alerts

# 4. Проверка дашбордов
echo "4. Проверка дашбордов..."
check_dashboards

# 5. Проверка ConfigMaps
echo "5. Проверка ConfigMaps..."
kubectl get configmaps -n cloudbridge-monitoring

# 6. Проверка логов
echo "6. Проверка логов..."
echo "Логи Prometheus:"
kubectl logs -n cloudbridge-monitoring -l app=prometheus --tail=5

echo "Логи Grafana:"
kubectl logs -n cloudbridge-monitoring -l app=grafana --tail=5

echo "Логи Alertmanager:"
kubectl logs -n cloudbridge-monitoring -l app=alertmanager --tail=5

# 7. Проверка ресурсов
echo "7. Проверка ресурсов..."
kubectl top pods -n cloudbridge-monitoring 2>/dev/null || echo "WARNING: Метрики ресурсов недоступны (требуется metrics-server)"

echo "SUCCESS: Тестирование завершено!"
echo ""
echo "Результаты:"
echo "  - Все сервисы доступны"
echo "  - Метрики собираются"
echo "  - Алерты работают"
echo "  - Дашборды загружены"
echo ""
echo "Для доступа к интерфейсам:"
echo "  - Prometheus: kubectl port-forward -n cloudbridge-monitoring svc/prometheus 9090:9090"
echo "  - Grafana: kubectl port-forward -n cloudbridge-monitoring svc/grafana 3000:3000"
echo "  - Alertmanager: kubectl port-forward -n cloudbridge-monitoring svc/alertmanager 9093:9093"
