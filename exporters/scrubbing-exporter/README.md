# CloudBridge Scrubbing Exporter

Prometheus exporter for CloudBridge DDoS protection and scrubbing metrics.

## Features

- **DDoS attack detection metrics** with tenant/PoP labels
- **BGP blackhole injection tracking**
- **Upstream scrubbing provider monitoring**
- **Alertmanager webhook integration**
- **Multi-tenancy support**

## Metrics

### `cloudbridge_scrubbing_ddos_attacks_detected_total`
Total number of DDoS attacks detected.

Labels: `tenant`, `pop`, `attack_type`

### `cloudbridge_scrubbing_bgp_blackholes_injected_total`
Total number of BGP blackhole routes injected.

Labels: `tenant`, `pop`, `prefix`

### `cloudbridge_scrubbing_latency_seconds`
Scrubbing response time histogram.

Labels: `tenant`, `pop`, `provider`

### `cloudbridge_scrubbing_bgp_blackholes_active`
Number of currently active BGP blackhole routes.

Labels: `tenant`, `pop`

### `cloudbridge_scrubbing_upstream_provider_status`
Upstream scrubbing provider status (1=up, 0=down).

Labels: `provider`

## Configuration

Environment variables:

- `YANDEX_CLOUD_ENABLED`: Enable Yandex Cloud monitoring (default: false)
- `QRATOR_ENABLED`: Enable Qrator monitoring (default: false)
- `RTK_ENABLED`: Enable RTK monitoring (default: false)

## API Endpoints

- `GET /metrics`: Prometheus metrics
- `GET /health`: Health check
- `POST /alert`: Alertmanager webhook

## Building

```bash
go build -o scrubbing-exporter .
```

## Running

```bash
./scrubbing-exporter
```

## Docker

```bash
docker build -t cloudbridge/scrubbing-exporter:latest .
docker run -p 8080:8080 cloudbridge/scrubbing-exporter:latest
```

## Kubernetes Deployment

See `k8s/` directory for deployment manifests.
