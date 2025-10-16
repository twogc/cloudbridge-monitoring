#!/bin/bash
# CloudBridge Monitoring Disaster Recovery Scripts

set -e

NAMESPACE="cloudbridge-monitoring"
BACKUP_DIR="/backups/cloudbridge-monitoring"
RESTORE_NAMESPACE="cloudbridge-monitoring-restore"

echo "CloudBridge Monitoring Disaster Recovery"

# 1. Восстановление Grafana
restore_grafana() {
    local backup_file=$1
    
    if [ -z "$backup_file" ]; then
        echo "ERROR: Backup file not specified"
        return 1
    fi
    
    echo "Restoring Grafana from $backup_file..."
    
    # Создаем namespace для восстановления
    kubectl create namespace $RESTORE_NAMESPACE --dry-run=client -o yaml | kubectl apply -f -
    
    # Разворачиваем PostgreSQL
    kubectl apply -f grafana/grafana-ha-resilient.yaml -n $RESTORE_NAMESPACE
    
    # Ждем готовности PostgreSQL
    kubectl wait --for=condition=ready pod -l app=postgres -n $RESTORE_NAMESPACE --timeout=300s
    
    # Получаем имя пода PostgreSQL
    POSTGRES_POD=$(kubectl get pods -n $RESTORE_NAMESPACE -l app=postgres -o jsonpath='{.items[0].metadata.name}')
    
    # Восстанавливаем базу данных
    if [[ $backup_file == *.gz ]]; then
        gunzip -c $backup_file | kubectl exec -i -n $RESTORE_NAMESPACE $POSTGRES_POD -- psql -U grafana grafana
    else
        kubectl exec -i -n $RESTORE_NAMESPACE $POSTGRES_POD -- psql -U grafana grafana < $backup_file
    fi
    
    echo "Grafana restore completed"
}

# 2. Восстановление Alertmanager
restore_alertmanager() {
    local silences_file=$1
    local alerts_file=$2
    
    echo "Restoring Alertmanager..."
    
    # Разворачиваем Alertmanager
    kubectl apply -f alertmanager/alertmanager-ha-resilient.yaml -n $RESTORE_NAMESPACE
    
    # Ждем готовности Alertmanager
    kubectl wait --for=condition=ready pod -l app=alertmanager -n $RESTORE_NAMESPACE --timeout=300s
    
    # Восстанавливаем silences
    if [ -n "$silences_file" ]; then
        kubectl port-forward -n $RESTORE_NAMESPACE svc/alertmanager 9093:9093 &
        PF_PID=$!
        sleep 5
        
        # Импортируем silences
        if [[ $silences_file == *.gz ]]; then
            gunzip -c $silences_file | curl -X POST -H "Content-Type: application/json" -d @- http://localhost:9093/api/v1/silences
        else
            curl -X POST -H "Content-Type: application/json" -d @$silences_file http://localhost:9093/api/v1/silences
        fi
        
        kill $PF_PID
    fi
    
    echo "Alertmanager restore completed"
}

# 3. Восстановление VictoriaMetrics
restore_victoria_metrics() {
    local backup_path=$1
    
    echo "Restoring VictoriaMetrics from $backup_path..."
    
    # Разворачиваем VictoriaMetrics
    kubectl apply -f victoria-metrics/victoria-metrics.yaml -n $RESTORE_NAMESPACE
    
    # Ждем готовности VictoriaMetrics
    kubectl wait --for=condition=ready pod -l app=victoria-metrics -n $RESTORE_NAMESPACE --timeout=300s
    
    # Получаем имя пода VictoriaMetrics
    VM_POD=$(kubectl get pods -n $RESTORE_NAMESPACE -l app=victoria-metrics -o jsonpath='{.items[0].metadata.name}')
    
    # Восстанавливаем данные
    kubectl exec -n $RESTORE_NAMESPACE $VM_POD -- vmrestore -src=$backup_path -storageDataPath=/victoria-metrics-data
    
    echo "VictoriaMetrics restore completed"
}

# 4. Восстановление конфигураций
restore_configs() {
    local configs_file=$1
    
    echo "Restoring configurations from $configs_file..."
    
    # Восстанавливаем ConfigMaps и Secrets
    kubectl apply -f $configs_file -n $RESTORE_NAMESPACE
    
    echo "Configurations restore completed"
}

# 5. Восстановление Prometheus rules
restore_prometheus_rules() {
    local rules_file=$1
    
    echo "Restoring Prometheus rules from $rules_file..."
    
    # Разворачиваем Prometheus
    kubectl apply -f prometheus/prometheus-ha-resilient.yaml -n $RESTORE_NAMESPACE
    
    # Ждем готовности Prometheus
    kubectl wait --for=condition=ready pod -l app=prometheus -n $RESTORE_NAMESPACE --timeout=300s
    
    # Импортируем правила
    kubectl port-forward -n $RESTORE_NAMESPACE svc/prometheus 9090:9090 &
    PF_PID=$!
    sleep 5
    
    # Импортируем правила через API
    if [[ $rules_file == *.gz ]]; then
        gunzip -c $rules_file | curl -X POST -H "Content-Type: application/json" -d @- http://localhost:9090/api/v1/rules
    else
        curl -X POST -H "Content-Type: application/json" -d @$rules_file http://localhost:9090/api/v1/rules
    fi
    
    kill $PF_PID
    
    echo "Prometheus rules restore completed"
}

# 6. Полное восстановление
full_restore() {
    local backup_date=$1
    
    if [ -z "$backup_date" ]; then
        echo "ERROR: Backup date not specified"
        echo "Usage: $0 full-restore YYYYMMDD_HHMMSS"
        return 1
    fi
    
    echo "Starting full restore from backup $backup_date..."
    
    # Находим файлы бэкапа
    GRAFANA_BACKUP=$(find $BACKUP_DIR -name "grafana_${backup_date}.sql.gz" | head -1)
    SILENCES_BACKUP=$(find $BACKUP_DIR -name "alertmanager_silences_${backup_date}.json.gz" | head -1)
    CONFIGS_BACKUP=$(find $BACKUP_DIR -name "configmaps_${backup_date}.yaml" | head -1)
    RULES_BACKUP=$(find $BACKUP_DIR -name "prometheus_rules_${backup_date}.json.gz" | head -1)
    
    # Проверяем наличие файлов
    if [ ! -f "$GRAFANA_BACKUP" ]; then
        echo "ERROR: Grafana backup not found: $GRAFANA_BACKUP"
        return 1
    fi
    
    if [ ! -f "$CONFIGS_BACKUP" ]; then
        echo "ERROR: Configs backup not found: $CONFIGS_BACKUP"
        return 1
    fi
    
    # Восстанавливаем компоненты
    restore_configs "$CONFIGS_BACKUP"
    restore_grafana "$GRAFANA_BACKUP"
    restore_alertmanager "$SILENCES_BACKUP"
    restore_prometheus_rules "$RULES_BACKUP"
    
    echo "Full restore completed"
}

# 7. Тестирование восстановления
test_restore() {
    echo "Testing restored monitoring stack..."
    
    # Проверяем доступность сервисов
    kubectl get pods -n $RESTORE_NAMESPACE
    
    # Проверяем Prometheus
    kubectl port-forward -n $RESTORE_NAMESPACE svc/prometheus 9090:9090 &
    PF_PID1=$!
    sleep 5
    
    curl -s http://localhost:9090/api/v1/query?query=up | jq -r '.data.result[] | select(.metric.job=="prometheus")'
    
    kill $PF_PID1
    
    # Проверяем Grafana
    kubectl port-forward -n $RESTORE_NAMESPACE svc/grafana 3000:3000 &
    PF_PID2=$!
    sleep 5
    
    curl -s http://localhost:3000/api/health | jq -r '.database'
    
    kill $PF_PID2
    
    # Проверяем Alertmanager
    kubectl port-forward -n $RESTORE_NAMESPACE svc/alertmanager 9093:9093 &
    PF_PID3=$!
    sleep 5
    
    curl -s http://localhost:9093/api/v1/status | jq -r '.data.cluster.peers'
    
    kill $PF_PID3
    
    echo "Restore test completed"
}

# 8. Очистка тестового namespace
cleanup_restore() {
    echo "Cleaning up restore namespace..."
    
    kubectl delete namespace $RESTORE_NAMESPACE
    
    echo "Cleanup completed"
}

# Основная функция
main() {
    case "$1" in
        "grafana")
            restore_grafana "$2"
            ;;
        "alertmanager")
            restore_alertmanager "$2" "$3"
            ;;
        "victoria-metrics")
            restore_victoria_metrics "$2"
            ;;
        "configs")
            restore_configs "$2"
            ;;
        "prometheus")
            restore_prometheus_rules "$2"
            ;;
        "full-restore")
            full_restore "$2"
            ;;
        "test")
            test_restore
            ;;
        "cleanup")
            cleanup_restore
            ;;
        *)
            echo "Usage: $0 {grafana|alertmanager|victoria-metrics|configs|prometheus|full-restore|test|cleanup} [backup_file]"
            echo ""
            echo "Examples:"
            echo "  $0 full-restore 20240101_120000    # Full restore from backup"
            echo "  $0 grafana grafana_20240101_120000.sql.gz  # Restore Grafana only"
            echo "  $0 test                            # Test restored stack"
            echo "  $0 cleanup                         # Cleanup test namespace"
            exit 1
            ;;
    esac
}

main "$@"
