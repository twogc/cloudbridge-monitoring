# CloudBridge DDoS Protection Guide

## Overview

CloudBridge DDoS Protection provides enterprise-grade DDoS mitigation through:

- **Automated DDoS Detection**: Real-time traffic analysis with <30s detection time
- **BGP Blackhole Injection**: RFC 7999-compliant blackhole routes via GoBGP v4
- **Upstream Scrubbing Integration**: BGP peering with Yandex Cloud, Qrator Labs
- **Multi-Tenancy Support**: Tenant and PoP-level isolation and metrics
- **Comprehensive Monitoring**: Prometheus metrics and Grafana dashboards

## Architecture

```
┌─────────────────┐
│   DDoS Attack   │
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────────────┐
│             CloudBridge Edge PoPs                   │
│  ┌──────────────────┐      ┌──────────────────┐   │
│  │ Traffic Analysis │─────▶│ Prometheus       │   │
│  │ (Relay Metrics)  │      │ (DDoS Detection) │   │
│  └──────────────────┘      └─────────┬────────┘   │
└────────────────────────────────────────┼───────────┘
                                         │
                   ┌─────────────────────┼─────────────────────┐
                   │                     ▼                     │
                   │           ┌──────────────────┐            │
                   │           │  Alertmanager    │            │
                   │           │  (Alert Firing)  │            │
                   │           └─────────┬────────┘            │
                   │                     │                     │
         ┌─────────┼─────────────────────┼─────────┬───────────┼──────────┐
         │         │                     │         │           │          │
         ▼         ▼                     ▼         ▼           ▼          ▼
┌────────────────────┐         ┌──────────────────────┐  ┌─────────────────┐
│ Scrubbing Exporter │         │ BGP Blackhole        │  │ Grafana         │
│ (Metrics)          │         │ Controller           │  │ (Visualization) │
└────────────────────┘         └──────────┬───────────┘  └─────────────────┘
                                          │
                                          ▼
                               ┌──────────────────┐
                               │  GoBGP v4 API    │
                               │  (BGP Routes)    │
                               └─────────┬────────┘
                                         │
                                         ▼
                               ┌──────────────────┐
                               │ BGP Network      │
                               │ (Blackhole/      │
                               │  Upstream        │
                               │  Scrubbing)      │
                               └──────────────────┘
```

## Components

### 1. Scrubbing Exporter

**Purpose**: Collects DDoS protection metrics and monitors upstream scrubbing providers.

**Metrics**:
- `cloudbridge_scrubbing_ddos_attacks_detected_total`: Total attacks detected
- `cloudbridge_scrubbing_upstream_provider_status`: Provider health (1=up, 0=down)
- `cloudbridge_scrubbing_latency_seconds`: Scrubbing response time

**Configuration**: `/exporters/scrubbing-exporter/k8s/deployment.yaml`

### 2. BGP Blackhole Controller

**Purpose**: Injects and withdraws BGP blackhole routes via GoBGP v4 API.

**Features**:
- RFC 7999 blackhole community (65535:666)
- Automatic route withdrawal after TTL
- Alertmanager webhook integration
- Multi-tenancy with labels

**Metrics**:
- `cloudbridge_bgp_blackhole_injected_total`: Total blackholes injected
- `cloudbridge_bgp_blackhole_active`: Currently active blackholes
- `cloudbridge_bgp_blackhole_duration_seconds`: Blackhole duration histogram

**Configuration**: `/exporters/bgp-blackhole-exporter/k8s/deployment.yaml`

## Deployment

### Prerequisites

- Kubernetes cluster with `kubectl` configured
- GoBGP running in `cloudbridge-dns-network` namespace
- Prometheus and Alertmanager deployed
- Docker for building images

### Quick Start

```bash
cd /Users/maxlanies/Git/2GC/cloudbridge-relay-installer/oss-repositories/cloudbridge-monitoring
./scripts/deploy-scrubbing.sh
```

This script will:
1. Create `cloudbridge-scrubbing` namespace
2. Apply RBAC and NetworkPolicies
3. Build Docker images (optional)
4. Deploy exporters
5. Configure Prometheus scraping and alerting
6. Import Grafana dashboard

### Manual Deployment

#### Step 1: Namespace and RBAC

```bash
kubectl apply -f security/scrubbing-namespace.yaml
kubectl apply -f security/scrubbing-rbac.yaml
kubectl apply -f security/scrubbing-network-policy.yaml
```

#### Step 2: Build Images

```bash
# Scrubbing exporter
cd exporters/scrubbing-exporter
docker build -t cloudbridge/scrubbing-exporter:latest .
docker push cloudbridge/scrubbing-exporter:latest

# BGP blackhole exporter
cd ../bgp-blackhole-exporter
docker build -t cloudbridge/bgp-blackhole-exporter:latest .
docker push cloudbridge/bgp-blackhole-exporter:latest
```

#### Step 3: Deploy Exporters

```bash
kubectl apply -f exporters/scrubbing-exporter/k8s/deployment.yaml
kubectl apply -f exporters/bgp-blackhole-exporter/k8s/deployment.yaml
```

#### Step 4: Configure Monitoring

```bash
kubectl apply -f prometheus/scrubbing-scrape-config.yaml
kubectl apply -f prometheus/scrubbing-rules.yaml
kubectl create configmap grafana-dashboard-scrubbing \
    --from-file=grafana/scrubbing-dashboard.json \
    -n cloudbridge-monitoring
```

## Configuration

### API Keys (Optional)

For upstream scrubbing providers, configure API keys:

```bash
# Edit the secret
kubectl edit secret scrubbing-secrets -n cloudbridge-scrubbing

# Add base64-encoded keys:
# QRATOR_API_KEY: <base64-encoded-key>
# YANDEX_CLOUD_API_KEY: <base64-encoded-key>
# RTK_API_KEY: <base64-encoded-key>
```

### GoBGP Connection

The BGP blackhole controller connects to GoBGP via gRPC:

```yaml
env:
- name: GOBGP_ADDR
  value: "gobgp.cloudbridge-dns-network:50051"
```

Update this if your GoBGP service name differs.

### Alertmanager Webhook

Configure Alertmanager to send DDoS alerts to the blackhole controller:

```yaml
# alertmanager.yml
receivers:
- name: 'bgp-blackhole'
  webhook_configs:
  - url: 'http://bgp-blackhole-exporter.cloudbridge-scrubbing:8080/alert'
    send_resolved: true
```

## Monitoring

### Grafana Dashboard

Access the DDoS Protection dashboard:

```
http://<grafana-url>/d/scrubbing
```

**Panels**:
- DDoS attacks detected (24h)
- Active BGP blackholes
- Upstream scrubbing status
- Attack rate by type
- Blackhole injection timeline
- Scrubbing latency P95

### Key Metrics

#### Detection Metrics

```promql
# Attack rate (5m)
rate(cloudbridge_scrubbing_ddos_attacks_detected_total[5m])

# Attacks by type
sum by (attack_type) (rate(cloudbridge_scrubbing_ddos_attacks_detected_total[5m]))

# Attacks by tenant
cloudbridge:ddos_attacks:by_tenant
```

#### Mitigation Metrics

```promql
# Active blackholes
sum(cloudbridge_bgp_blackhole_active)

# Blackhole injection rate
rate(cloudbridge_bgp_blackhole_injected_total[5m])

# Mitigation effectiveness
cloudbridge:blackhole:effectiveness_ratio
```

### Alerts

Key alerts configured in `/prometheus/scrubbing-rules.yaml`:

- **HighTrafficDetected**: Traffic >100Mbps for 30s
- **HighConnectionRate**: >1000 conn/s for 30s
- **UDPAmplificationDetected**: UDP amplification ratio >10x
- **BGPBlackholeInjected**: Blackhole route injected
- **UpstreamScrubbingDown**: Provider unavailable
- **TooManyActiveBlackholes**: >10 active blackholes

## Troubleshooting

### Scrubbing Exporter Issues

**Check logs**:
```bash
kubectl logs -n cloudbridge-scrubbing -l app=scrubbing-exporter -f
```

**Test health**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- curl -s http://localhost:8080/health
```

**View metrics**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- curl -s http://localhost:8080/metrics | grep cloudbridge_scrubbing
```

### BGP Blackhole Controller Issues

**Check GoBGP connection**:
```bash
kubectl logs -n cloudbridge-scrubbing -l app=bgp-blackhole-exporter -f | grep "GoBGP"
```

**Verify health**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/bgp-blackhole-exporter -- curl -s http://localhost:8080/health
```

**View active blackholes in BGP table**:
```bash
kubectl exec -n cloudbridge-dns-network <gobgp-pod> -- gobgpcli global rib
```

**Check for blackhole community (65535:666)**:
```bash
kubectl exec -n cloudbridge-dns-network <gobgp-pod> -- gobgpcli global rib -a ipv4 | grep 65535:666
```

### Alertmanager Webhook Not Triggering

**Test webhook manually**:
```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -X POST http://bgp-blackhole-exporter.cloudbridge-scrubbing:8080/alert \
  -H "Content-Type: application/json" \
  -d '{
    "alerts": [{
      "status": "firing",
      "labels": {
        "tenant": "test",
        "pop": "MOW",
        "target_ip": "1.2.3.4"
      }
    }]
  }'
```

**Check Alertmanager logs**:
```bash
kubectl logs -n cloudbridge-monitoring -l app=alertmanager | grep bgp-blackhole
```

### Metrics Not Appearing in Prometheus

**Verify Prometheus scrape targets**:
```
http://<prometheus-url>/targets
```

Look for:
- `scrubbing-exporter` (cloudbridge-scrubbing namespace)
- `bgp-blackhole-exporter` (cloudbridge-scrubbing namespace)

**Check NetworkPolicies**:
```bash
kubectl get networkpolicies -n cloudbridge-scrubbing
```

Ensure `allow-prometheus-scrape-*` policies exist.

## Security

### NetworkPolicies

All components are protected by NetworkPolicies:

- Prometheus can scrape metrics (port 8080)
- Alertmanager can send webhooks (port 8080)
- BGP blackhole controller can access GoBGP (port 50051)
- DNS queries allowed to kube-dns

### RBAC

ServiceAccount `scrubbing-controller` has minimal permissions:
- Read secrets/configmaps
- List services/endpoints
- Watch deployments

### Secrets Management

API keys stored in Kubernetes Secret:
```bash
kubectl get secret scrubbing-secrets -n cloudbridge-scrubbing -o yaml
```

## Performance

### Detection Time
- **Target**: <30 seconds from attack start to detection
- **Measured**: Prometheus evaluation interval (30s) + alert firing delay

### Mitigation Time
- **Target**: <60 seconds from detection to blackhole injection
- **Components**:
  - Alertmanager webhook delivery: <5s
  - BGP blackhole injection: <5s
  - BGP convergence: <30s

### Capacity
- **Protection**: 30-60 Gbps (depends on upstream providers)
- **Blackhole routes**: Unlimited (GoBGP scales to 100k+ routes)
- **Tenants**: Unlimited (metrics labeled by tenant/PoP)

## Upstream Integration

### Yandex Cloud DDoS Protection

**Method**: BGP peering + Terraform management

**Setup**:
1. Enable Yandex Cloud DDoS Protection via UI or Terraform
2. Configure BGP peering with GoBGP
3. Announce protected prefixes via BGP

**Monitoring**: API-based (read-only)

### Qrator Labs

**Method**: BGP peering

**Setup**:
1. Establish BGP session with Qrator
2. Announce prefixes to Qrator AS
3. Qrator filters attack traffic, forwards clean traffic

**API**: Monitoring and statistics only

### RTK (Ростелеком)

**Method**: Manual coordination

**Setup**:
1. Contact RTK for DDoS protection contract
2. Coordinate prefix announcements
3. Manual activation during attacks

## Support

- **Documentation**: This guide
- **Logs**: `kubectl logs -n cloudbridge-scrubbing`
- **Metrics**: Prometheus/Grafana dashboards
- **Issues**: GitHub repository

## References

- [RFC 7999: BLACKHOLE Community](https://datatracker.ietf.org/doc/html/rfc7999)
- [GoBGP v4 Documentation](https://github.com/osrg/gobgp)
- [Prometheus Alerting](https://prometheus.io/docs/alerting/latest/)
- [Yandex Cloud DDoS Protection](https://cloud.yandex.com/en/docs/vpc/ddos-protection/)
- [Qrator Labs API](https://docs.qrator.net/)
