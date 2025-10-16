#!/bin/bash
# CloudBridge Monitoring Backup Scripts

set -e

NAMESPACE="cloudbridge-monitoring"
BACKUP_DIR="/backups/cloudbridge-monitoring"
DATE=$(date +%Y%m%d_%H%M%S)

# Создание директории для бэкапов
mkdir -p $BACKUP_DIR

echo "Starting CloudBridge Monitoring backup at $(date)"

# 1. Backup Grafana (PostgreSQL)
backup_grafana() {
    echo "Backing up Grafana database..."
    
    # Получаем имя пода PostgreSQL
    POSTGRES_POD=$(kubectl get pods -n $NAMESPACE -l app=postgres -o jsonpath='{.items[0].metadata.name}')
    
    if [ -z "$POSTGRES_POD" ]; then
        echo "ERROR: PostgreSQL pod not found"
        return 1
    fi
    
    # Создаем дамп базы данных
    kubectl exec -n $NAMESPACE $POSTGRES_POD -- pg_dump -U grafana grafana > $BACKUP_DIR/grafana_$DATE.sql
    
    # Сжимаем дамп
    gzip $BACKUP_DIR/grafana_$DATE.sql
    
    echo "Grafana backup completed: grafana_$DATE.sql.gz"
}

# 2. Backup Alertmanager (silences)
backup_alertmanager() {
    echo "Backing up Alertmanager silences..."
    
    # Получаем silences из Alertmanager API
    kubectl port-forward -n $NAMESPACE svc/alertmanager 9093:9093 &
    PF_PID=$!
    sleep 5
    
    # Экспортируем silences
    curl -s http://localhost:9093/api/v1/silences > $BACKUP_DIR/alertmanager_silences_$DATE.json
    
    # Экспортируем notification log
    curl -s http://localhost:9093/api/v1/alerts > $BACKUP_DIR/alertmanager_alerts_$DATE.json
    
    kill $PF_PID
    
    # Сжимаем файлы
    gzip $BACKUP_DIR/alertmanager_silences_$DATE.json
    gzip $BACKUP_DIR/alertmanager_alerts_$DATE.json
    
    echo "Alertmanager backup completed"
}

# 3. Backup VictoriaMetrics
backup_victoria_metrics() {
    echo "Backing up VictoriaMetrics..."
    
    # Получаем имя пода VictoriaMetrics
    VM_POD=$(kubectl get pods -n $NAMESPACE -l app=victoria-metrics -o jsonpath='{.items[0].metadata.name}')
    
    if [ -z "$VM_POD" ]; then
        echo "ERROR: VictoriaMetrics pod not found"
        return 1
    fi
    
    # Создаем снапшот VictoriaMetrics
    kubectl exec -n $NAMESPACE $VM_POD -- vmbackup -storageDataPath=/victoria-metrics-data -dst=s3://cloudbridge-monitoring-backup/vm-backup-$DATE/
    
    echo "VictoriaMetrics backup completed"
}

# 4. Backup ConfigMaps и Secrets
backup_configs() {
    echo "Backing up ConfigMaps and Secrets..."
    
    # Backup ConfigMaps
    kubectl get configmaps -n $NAMESPACE -o yaml > $BACKUP_DIR/configmaps_$DATE.yaml
    
    # Backup Secrets (без данных)
    kubectl get secrets -n $NAMESPACE -o yaml > $BACKUP_DIR/secrets_$DATE.yaml
    
    # Backup все манифесты
    kubectl get all -n $NAMESPACE -o yaml > $BACKUP_DIR/resources_$DATE.yaml
    
    echo "Configs backup completed"
}

# 5. Backup Prometheus rules
backup_prometheus_rules() {
    echo "Backing up Prometheus rules..."
    
    # Экспортируем правила из Prometheus
    kubectl port-forward -n $NAMESPACE svc/prometheus 9090:9090 &
    PF_PID=$!
    sleep 5
    
    curl -s http://localhost:9090/api/v1/rules > $BACKUP_DIR/prometheus_rules_$DATE.json
    
    kill $PF_PID
    
    echo "Prometheus rules backup completed"
}

# 6. Очистка старых бэкапов (старше 30 дней)
cleanup_old_backups() {
    echo "Cleaning up old backups..."
    
    find $BACKUP_DIR -name "*.gz" -mtime +30 -delete
    find $BACKUP_DIR -name "*.yaml" -mtime +30 -delete
    find $BACKUP_DIR -name "*.json" -mtime +30 -delete
    
    echo "Old backups cleanup completed"
}

# 7. Проверка целостности бэкапов
verify_backups() {
    echo "Verifying backups..."
    
    # Проверяем что файлы существуют и не пустые
    for file in $BACKUP_DIR/*_$DATE.*; do
        if [ -f "$file" ] && [ -s "$file" ]; then
            echo "✓ $file is valid"
        else
            echo "✗ $file is invalid or missing"
            return 1
        fi
    done
    
    echo "Backup verification completed"
}

# Основная функция
main() {
    case "$1" in
        "grafana")
            backup_grafana
            ;;
        "alertmanager")
            backup_alertmanager
            ;;
        "victoria-metrics")
            backup_victoria_metrics
            ;;
        "configs")
            backup_configs
            ;;
        "prometheus")
            backup_prometheus_rules
            ;;
        "all")
            backup_grafana
            backup_alertmanager
            backup_victoria_metrics
            backup_configs
            backup_prometheus_rules
            cleanup_old_backups
            verify_backups
            ;;
        "cleanup")
            cleanup_old_backups
            ;;
        "verify")
            verify_backups
            ;;
        *)
            echo "Usage: $0 {grafana|alertmanager|victoria-metrics|configs|prometheus|all|cleanup|verify}"
            echo ""
            echo "Examples:"
            echo "  $0 all                    # Full backup"
            echo "  $0 grafana               # Backup Grafana only"
            echo "  $0 cleanup               # Clean old backups"
            echo "  $0 verify                # Verify backups"
            exit 1
            ;;
    esac
}

main "$@"
