# CloudBridge DDoS Protection Guide

## Overview

CloudBridge DDoS Protection provides enterprise-grade DDoS mitigation through:

- **Automated DDoS Detection**: Real-time traffic analysis with <30s detection time
- **BGP Blackhole Injection**: RFC 7999-compliant blackhole routes via GoBGP v4
- **Upstream Scrubbing Integration**: BGP peering with Yandex Cloud, Qrator Labs
- **Multi-Tenancy Support**: Tenant and PoP-level isolation and metrics
- **Comprehensive Monitoring**: Prometheus metrics and Grafana dashboards
- **High-Scale Support**: Up to 800k RPS with multi-layer protection stack

## Architecture

```
┌─────────────────┐
│ DDoS Attack │
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────────────────────────┐
│ CloudBridge Edge PoPs │
│ ┌──────────────────┐ ┌──────────────────┐ │
│ │ Traffic Analysis │─────▶│ Prometheus │ │
│ │ (Relay Metrics) │ │ (DDoS Detection) │ │
│ └──────────────────┘ └─────────┬────────┘ │
└────────────────────────────────────────┼───────────┘
                                         │
                   ┌─────────────────────┼─────────────────────┐
                   │ ▼ │
                   │ ┌──────────────────┐ │
                   │ │ Alertmanager │ │
                   │ │ (Alert Firing) │ │
                   │ └─────────┬────────┘ │
                   │ │ │
         ┌─────────┼─────────────────────┼─────────┬───────────┼──────────┐
         │ │ │ │ │ │
         ▼ ▼ ▼ ▼ ▼ ▼
┌────────────────────┐ ┌──────────────────────┐ ┌─────────────────┐
│ Scrubbing Exporter │ │ BGP Blackhole │ │ Grafana │
│ (Metrics) │ │ Controller │ │ (Visualization) │
└────────────────────┘ └──────────┬───────────┘ └─────────────────┘
                                          │
                                          ▼
                               ┌──────────────────┐
                               │ GoBGP v4 API │
                               │ (BGP Routes) │
                               └─────────┬────────┘
                                         │
                                         ▼
                               ┌──────────────────┐
                               │ BGP Network │
                               │ (Blackhole/ │
                               │ Upstream │
                               │ Scrubbing) │
                               └──────────────────┘
```

## High-Scale Architecture (800k RPS)

### Performance Requirements
- **Target RPS**: 800,000 requests/second
- **Bandwidth**: 52-70 Gbps (8KB average request size + headers/overhead)
- **PoP Distribution**: 8-12 PoP nodes (70-100k RPS per PoP)
- **Detection Time**: <10-20 seconds
- **Mitigation Time**: <1-5 seconds (L1/L2), <30-60 seconds (RTBH)

### Why Edge-Level Protection is Critical

Kubernetes adds **user-space/overlay-overhead** (CNI, ingress, sidecars), therefore **L0-L2 mitigation must work on the host** before packets enter the cluster. Inside the cluster, only **L7 logic** should remain: per-tenant rate limits, circuit-breakers, shedder.

**Target Architecture**:
```
Internet
  → L0 Anycast+BGP (Edge PoP)
  → L1 XDP/eBPF (pps, QUIC-Initial sanity, token-bucket /32)
  → L2 nftables (L3/L4 limits, flowtable, banlists)
  → L3 L7 proxy (Nginx-QUIC/Envoy; QUIC Retry, 0-RTT off in DDOS MODE)
  → L4 Kubernetes apps (rate limit per tenant, circuit-breakers, SLO)
```

### Multi-Layer Protection Stack

#### L0 - Anycast + BGP Distribution
- **Anycast IPv4/IPv6** for automatic traffic distribution
- **RTBH (RFC 7999 65535:666)** on upstream providers
- **RTBH with NO_EXPORT community** (provider-specific)
- **BFD** for fast withdraw (if available)
- **BGP convergence**: 30-60 seconds

#### L1 - XDP/eBPF (Before kernel networking stack - XDP fast path)
- **XDP-drop** for attack signatures
- **Token-bucket per /32** for fast rate limiting
- **QUIC anomaly detection** (abnormal Initial size, invalid DCID/SCID)
- **UDP amplification protection** (drop excessive responses without valid tokens)
- **Goal**: Filter 80-90% of malicious traffic before kernel networking stack

#### L2 - nftables (Kernel-level L3/L4 filtering)
- **Per-/32 nft sets** with dynamic quotas
- **Flowtable** for fast packet processing
- **UDP pkt/s limits** per source
- **Auto-ban** 60-300s for violating sources
- **Bypass conntrack** for UDP/443 to reduce CPU load

#### L3 - L7 Edge Proxy (Envoy/Nginx-QUIC)
- **QUIC Retry** + disable **0-RTT** in "DDOS MODE" (toggle on/off)
- **Rate-limit service** with keys: `{tenant_id, path_group, ip_cidr_24, auth_state, ja3_hash}`
- **JWT/HMAC proof-of-legit** for SDK clients
- **L7 protection**: header/body limits, timeouts, keep-alive tuning
- **IPv6 support**: anycast v6, drop extension headers, /64 rate limiting

#### L4 - Application Layer (Kubernetes)
- **Per-tenant budgets** + circuit-breakers
- **Shedder** (429/503) for traffic prioritization
- **Soft degradation order**: anonymous → gray JA3/UA → low-priority paths → 0-RTT/features
- **Legit error rate p95 < 0.5%** in DDOS MODE

### Performance Comparison: Edge vs Kubernetes

| Component | In K8s (typical) | On Edge (host) | Notes |
|-----------|------------------|----------------|-------|
| **XDP/eBPF** | Not available | 10M+ pps | Must be on edge before cluster |
| **nftables flowtable** | △ (hostNetwork) | 1M+ pps | More efficient outside cluster |
| **L7 QUIC/HTTP3 proxy** | △ (ingress) | 300k-500k+ RPS/node | In K8s only if traffic is "cleaned" |
| **BGP/RTBH** | Not available | Native support | Network/router functions |
| **Rate-limit per tenant** | Suitable | Suitable | In K8s - business quotas and SLO |
| **Service Mesh** | △ Possible | — Not needed | Often disabled in DDoS scenarios |

> △ — Possible but with caveats (hostNetwork, eBPF-kube-proxy, irq-pinning, etc.)

**Note**: Performance numbers depend on configuration (hostNetwork, SO_REUSEPORT, irq-pinning, zero-copy, eBPF kube-proxy vs iptables, hardware NIC/NUMA). Values shown are typical orders of magnitude with standard settings.

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

## High-Scale Configuration

### Nginx-QUIC Configuration for 800k RPS

```nginx
# DDOS MODE toggle (enable during attacks)
quic_retry on; # Enable QUIC Retry in DDOS MODE
# quic_0rtt off; # Disable 0-RTT in DDOS MODE

# Rate limiting for 800k RPS
limit_req_zone $binary_remote_addr zone=api:50m rate=200r/s;
limit_req_zone $jwt_tenant zone=tenant:50m rate=2000r/s;

server {
    listen 443 quic reuseport;
    limit_req zone=api burst=100 nodelay;

    # L7 protection limits
    client_max_body_size 1m;
    client_header_buffer_size 4k;
    large_client_header_buffers 4 8k;
    client_body_timeout 10s;
    client_header_timeout 10s;
    keepalive_timeout 30s; # Aggressive in DDOS MODE
}
```

### Envoy HTTP/3 Configuration

```yaml
http3_protocol_options:
  max_header_list_size: 32768 # ~32 KB
  disable_0rtt: true # Enable in DDOS MODE
common_http_protocol_options:
  max_headers_count: 100
  max_stream_duration: 5s
  idle_timeout: 30s
route_config:
  request_headers_to_add:
  - header:
      key: x-cb-tenant
      value: "%DYNAMIC_METADATA(cb.tenant)%"
  request_headers_to_remove: ["x-forwarded-client-cert"]
```

### nftables Rules for High-Scale

```nft
table inet ddos {
    flowtable ft { hook ingress priority 0; devices = { eth0 }; }
    set badsrc { type ipv4_addr; timeout 5m; }
    set bad24 { type ipv4_addr; timeout 5m; }
    chain preraw {
        type filter hook prerouting priority -300; # before conntrack
        udp dport 443 notrack
    }
    chain ingress {
        type filter hook ingress device eth0 priority -500; # fast path
        ip saddr @badsrc drop
        udp dport 443 meter quic { ip saddr limit rate over 2000/second } add @badsrc { ip saddr } drop
        tcp flags syn meter syn { ip saddr limit rate over 200/second } add @badsrc { ip saddr } drop
        ip saddr @badsrc add @bad24 { ip saddr & 255.255.255.0 } # escalate to /24
    }
}
```

### XDP/eBPF Configuration

- **XDP drop profile** for QUIC anomalies
- **Token-bucket per /32** for rate limiting

### Host Tuning for Edge Nodes

To handle 70-100k RPS per PoP and high pps:

```bash
# IRQ pinning and RPS
ethtool -K eth0 gro on gso on tso on
echo f > /proc/irq/<IRQ>/smp_affinity
echo 32768 > /proc/sys/net/core/somaxconn
echo "4096 87380 33554432" > /proc/sys/net/ipv4/tcp_rmem
echo "4096 87380 33554432" > /proc/sys/net/ipv4/tcp_wmem
sysctl -w net.core.rmem_max=33554432 net.core.wmem_max=33554432
# UDP: increase buffers
sysctl -w net.core.netdev_max_backlog=250000
```
- **UDP pkt/s limits** per source

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

#### High-Scale Performance Metrics

```promql
# RPS per PoP (target: 70-100k RPS)
rate(cloudbridge_relay_requests_total[1m]) by (pop)

# Bandwidth per PoP (target: 8-10 Gbps)
rate(cloudbridge_relay_bytes_total[1m]) by (pop) * 8

# CPU utilization per PoP
cloudbridge_relay_cpu_usage_percent by (pop)

# Memory usage per PoP
cloudbridge_relay_memory_usage_bytes by (pop)

# QUIC/HTTP3 metrics
quic_retry_rate by (pop)
quic_h3_handshake_failures_total by (pop)

# XDP/nftables drop rates
xdp_drop_pps by (pop, protocol)
nft_drop_pps by (pop, protocol)

# Envoy overload protection
envoy_overload_active by (pop)
request_timeout_total by (pop)

# Rate limiting service
rls_enforce_rate by (pop, tenant)
rls_banlist_size by (pop)
rls_false_positive_total by (pop)

# RTBH metrics
rtbh_prefixes_active by (mask)

# Attack detection time
cloudbridge_ddos_detection_duration_seconds

# Mitigation effectiveness
cloudbridge_ddos_mitigation_effectiveness_ratio

# False positive rate
cloudbridge_ddos_false_positive_rate
```

### Alerts

Key alerts configured in `/prometheus/scrubbing-rules.yaml`:

- **HighTrafficDetected**: Traffic >100Mbps for 30s
- **HighConnectionRate**: >1000 conn/s for 30s
- **UDPAmplificationDetected**: UDP amplification ratio >10x
- **BGPBlackholeInjected**: Blackhole route injected
- **UpstreamScrubbingDown**: Provider unavailable
- **TooManyActiveBlackholes**: >10 active blackholes

#### High-Scale Alerts

- **HighRPSSpike**: RPS > 100k per PoP for 30s
- **HighBandwidthSpike**: Bandwidth > 10 Gbps per PoP for 30s
- **HighCPUUsage**: CPU > 80% per PoP for 5m
- **HighMemoryUsage**: Memory > 80% per PoP for 5m
- **MLAnomalyDetected**: AI anomaly score > 0.8 for 30s
- **BehavioralAnomaly**: Behavioral score > 0.7 for 30s
- **TrafficPatternAnomaly**: Traffic pattern score > 0.6 for 30s
- **XDPDropRate**: XDP drop rate > 50% for 1m
- **NftablesDropRate**: nftables drop rate > 30% for 1m
- **QUICRetryRate**: QUIC retry rate > 20% for 1m
- **OverloadOn**: envoy_overload_active==1 for 30s
- **HighRetry**: quic_retry_rate > 0.25 for 1m
- **RLSFailOpen**: RLS service unavailable → fallback policy

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

### High-Scale Performance Issues

**Check RPS per PoP**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_relay_requests_total
```

**Check bandwidth per PoP**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_relay_bytes_total
```

**Check CPU usage per PoP**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_relay_cpu_usage
```

**Check XDP drop rate**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_xdp_drop_rate
```

**Check nftables drop rate**:
```bash
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_nftables_drop_rate
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

### High-Scale SLO Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Detection Time** | < 10-20s | AI/ML anomaly detection |
| **L1/L2 Mitigation** | < 1-5s | XDP/nftables filtering |
| **RTBH Injection** | < 30-60s | BGP convergence |
| **False Positive Rate** | < 0.5% | Legitimate traffic errors |
| **PoP Capacity** | 70-100k RPS | Per PoP performance |
| **Total Capacity** | 800k RPS | System-wide performance |
| **Bandwidth** | 70 Gbps | Total traffic capacity |

### High-Scale Resource Requirements

#### Hardware per PoP
- **CPU**: 16-32 cores (for 70-100k RPS)
- **RAM**: 64-128 GB (for XDP/eBPF + applications)
- **Network**: 2×100 GbE (for redundancy and capacity)
- **Storage**: NVMe SSD (for caching and logs)
- **L7 nodes**: SO_REUSEPORT + irq/CPU pinning required

#### Total Infrastructure
- **PoP Nodes**: 8-12 nodes
- **Monthly Cost**: ~$500K (hardware + upstream scrubbing)
- **Implementation Time**: 2-3 months
- **Maintenance**: 24/7 monitoring and response

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

**Method**: Manual coordination + RTBH/scrubbing by contract

**Setup**:
1. Contact RTK for DDoS protection contract
2. Coordinate prefix announcements
3. RTBH and scrubbing **by contract**; some scenarios require manual activation

## High-Scale Deployment Plan

### Phase 1: Infrastructure (Week 1-2)
1. **Deploy 8-12 PoP nodes** with 2×100 GbE each
2. **Configure anycast** for automatic load distribution
3. **Setup XDP/eBPF** filtering on each PoP
4. **Deploy nftables** rules for L3/L4 filtering

### Phase 2: Application Layer (Week 3-4)
1. **Configure Nginx-QUIC** with retry and rate limiting
2. **Deploy Envoy RLS** for rate limiting service
3. **Setup JWT/HMAC** authentication for SDK clients
4. **Configure circuit-breakers** and shedder

### Phase 3: Monitoring & AI (Week 5-6)
1. **Deploy AI/ML detection** with CloudBridge AI Service
2. **Configure Prometheus** alerts for high-scale metrics
3. **Setup Grafana** dashboards for 800k RPS monitoring
4. **Test end-to-end** protection with load testing

### Phase 4: Upstream Integration (Week 7-8)
1. **Configure RTBH** with Qrator/Yandex/RTK
2. **Setup upstream scrubbing** for 70+ Gbps capacity
3. **Test BGP blackhole** injection and withdrawal
4. **Validate end-to-end** protection

## DDOS MODE Runbook

### Enable DDOS MODE (Attack Response)

1. **Enable Protection**:
   - **QUIC**: `retry on`, `0-RTT off`, smaller initial cwnd
   - **L7**: RLS keys `{tenant, path_group, /24, ja3, auth_state}`, strict timeouts
   - **L2**: nftables flowtable + per-/32 meters (UDP pkt/s, SYN/s) → auto-ban 60-300s
   - **L1**: XDP drop abnormal QUIC Initial, token-bucket /32
   - **BGP**: RTBH `/32 → /24` by escalation, optionally `NO_EXPORT`

2. **Prioritize Traffic**:
   - Auth/API core endpoints first
   - Disable heavy endpoints/aggregations
   - Enable circuit-breakers

3. **Escalate Protection**:
   - Auto-RTBH by /32 → escalate to /24 if confirmed attack
   - Activate upstream scrubbing
   - Monitor false positives

4. **Observe Metrics**:
   - `legit_error_rate p95 < 0.5%`
   - `quic_retry_rate`
   - `xdp_drop_pps`, `nft_drop_pps`
   - `envoy_overload_active`

5. **Restore Normal Mode**:
   - Hysteresis: 10-15 minutes without anomalies
   - Gradual relaxation of limits
   - Monitor for attack recurrence

### RTBH Escalation Strategy

- **/32 RTBH**: Immediate response to confirmed attacks
- **/24 RTBH**: Escalation for sustained attacks
- **NO_EXPORT community**: Provider-specific requirements
- **BFD**: Fast withdraw when available

**RTBH Communities**:
```
RTBH communities:
- 65535:666 (BLACKHOLE, RFC 7999)
- <provider>:666 (provider-specific)
- NO_EXPORT (as required by upstream)
Escalation: /32 → /24 (only for confirmed attacks), rollback via BFD/timer
```

## Hybrid Architecture: Edge + Kubernetes

### Edge PoP (Bare Metal/VMs)
```
┌─────────────────────────────────────────────────────────┐
│ CloudBridge Edge PoP │
│ │
│ ┌─────────────────┐ ┌─────────────────────────────┐ │
│ │ Bare Metal │ │ Kubernetes Cluster │ │
│ │ │ │ │ │
│ │ • XDP/eBPF │────│ • Application Services │ │
│ │ • nftables │ │ • Rate Limiting │ │
│ │ • Nginx-QUIC │ │ • Circuit Breakers │ │
│ │ • BGP │ │ • Monitoring │ │
│ └─────────────────┘ └─────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

**Edge Responsibilities**:
- **L0-L2 Protection**: Anycast IPv4/IPv6, BGP, XDP/eBPF, nftables
- **High-performance filtering**: 10M+ pps, 1M+ pps
- **L7 Edge Proxy**: Nginx-QUIC/Envoy with DDOS MODE toggles
- **BGP Management**: RTBH injection/withdrawal
- **IPv6 Support**: anycast v6, drop extension headers, /64 rate limiting

**Kubernetes Responsibilities**:
- **L7 Business Logic**: Per-tenant rate limits, circuit-breakers
- **Application Services**: Core business functionality
- **Monitoring**: Metrics collection, alerting
- **Service Mesh**: Optional, often disabled during attacks

### Traffic Flow
```
Internet → Edge PoP (Bare Metal) → K8s Cluster (Apps)
         ↑ ↑
    DDoS Protection Business Logic
    (XDP/nftables) (Rate limiting)
```

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
