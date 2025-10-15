#!/bin/bash
# scripts/deploy-monitoring.sh

set -e

echo "📊 Развертывание CloudBridge Monitoring Stack"

# 1. Создание namespace
echo "📦 Создание namespace..."
kubectl create namespace cloudbridge-monitoring --dry-run=client -o yaml | kubectl apply -f -

# 2. Развертывание Prometheus
echo "📈 Развертывание Prometheus..."
kubectl apply -f prometheus/

# 3. Ожидание готовности Prometheus
echo "⏳ Ожидание готовности Prometheus..."
kubectl wait --for=condition=ready pod -l app=prometheus -n cloudbridge-monitoring --timeout=300s

# 4. Развертывание Grafana
echo "📊 Развертывание Grafana..."
kubectl apply -f grafana/

# 5. Ожидание готовности Grafana
echo "⏳ Ожидание готовности Grafana..."
kubectl wait --for=condition=ready pod -l app=grafana -n cloudbridge-monitoring --timeout=300s

# 6. Развертывание Alertmanager
echo "🚨 Развертывание Alertmanager..."
kubectl apply -f alertmanager/

# 7. Ожидание готовности Alertmanager
echo "⏳ Ожидание готовности Alertmanager..."
kubectl wait --for=condition=ready pod -l app=alertmanager -n cloudbridge-monitoring --timeout=300s

# 8. Проверка готовности
echo "✅ Проверка готовности Monitoring Stack..."
kubectl get pods -n cloudbridge-monitoring

echo "🎉 Monitoring Stack развернут успешно!"
echo ""
echo "📊 Доступные сервисы:"
echo "  - Prometheus: kubectl port-forward -n cloudbridge-monitoring svc/prometheus 9090:9090"
echo "  - Grafana: kubectl port-forward -n cloudbridge-monitoring svc/grafana 3000:3000"
echo "  - Alertmanager: kubectl port-forward -n cloudbridge-monitoring svc/alertmanager 9093:9093"
