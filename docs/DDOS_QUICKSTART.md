# CloudBridge DDoS Protection - Quick Start Guide

## 5-Minute Quick Start

### 1. Deploy DDoS Protection Stack

```bash
cd /Users/maxlanies/Git/2GC/cloudbridge-relay-installer/oss-repositories/cloudbridge-monitoring
./scripts/deploy-scrubbing.sh
```

### 2. Verify Deployment

```bash
# Check pods running
kubectl get pods -n cloudbridge-scrubbing

# Expected output:
# NAME                                      READY   STATUS    RESTARTS   AGE
# scrubbing-exporter-xxxxxxxxxx-xxxxx      1/1     Running   0          1m
# scrubbing-exporter-xxxxxxxxxx-xxxxx      1/1     Running   0          1m
# bgp-blackhole-exporter-xxxxxxxxxx-xxxxx  1/1     Running   0          1m
# bgp-blackhole-exporter-xxxxxxxxxx-xxxxx  1/1     Running   0          1m
```

### 3. Test Health Endpoints

```bash
# Scrubbing exporter
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/health

# BGP blackhole controller
kubectl exec -n cloudbridge-scrubbing deployment/bgp-blackhole-exporter -- \
  curl -s http://localhost:8080/health
```

Both should return: `OK`

### 4. View Metrics

```bash
# Scrubbing metrics
kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_scrubbing

# Blackhole metrics
kubectl exec -n cloudbridge-scrubbing deployment/bgp-blackhole-exporter -- \
  curl -s http://localhost:8080/metrics | grep cloudbridge_bgp_blackhole
```

### 5. Access Grafana Dashboard

Open Grafana and navigate to:
```
Dashboards → CloudBridge DDoS Protection & Scrubbing
```

Or direct URL:
```
http://<your-grafana-url>/d/scrubbing
```

## Test DDoS Detection (Optional)

### Simulate High Traffic Alert

```bash
# Create test alert
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -X POST http://bgp-blackhole-exporter.cloudbridge-scrubbing:8080/alert \
  -H "Content-Type: application/json" \
  -d '{
    "alerts": [{
      "status": "firing",
      "labels": {
        "tenant": "test-tenant",
        "pop": "MOW",
        "target_ip": "192.0.2.1",
        "attack_type": "syn_flood"
      },
      "annotations": {
        "summary": "Test DDoS attack"
      }
    }]
  }'
```

### Verify Blackhole Injection

```bash
# Check BGP table for blackhole route
kubectl exec -n cloudbridge-dns-network <gobgp-pod> -- \
  gobgpcli global rib -a ipv4 | grep 192.0.2.1

# Should show route with community 65535:666
```

### View in Grafana

Reload the Grafana dashboard - you should see:
- **DDoS Attacks Detected**: +1
- **Active BGP Blackholes**: +1
- **Attack timeline**: Spike at current time

### Withdraw Blackhole (Resolve Alert)

```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -X POST http://bgp-blackhole-exporter.cloudbridge-scrubbing:8080/alert \
  -H "Content-Type: application/json" \
  -d '{
    "alerts": [{
      "status": "resolved",
      "labels": {
        "tenant": "test-tenant",
        "pop": "MOW",
        "target_ip": "192.0.2.1"
      }
    }]
  }'
```

## Next Steps

1. **Configure API Keys** (if using upstream providers):
   ```bash
   kubectl edit secret scrubbing-secrets -n cloudbridge-scrubbing
   ```

2. **Update Alertmanager Configuration**:
   Add webhook receiver for automatic blackhole injection:
   ```yaml
   receivers:
   - name: 'bgp-blackhole'
     webhook_configs:
     - url: 'http://bgp-blackhole-exporter.cloudbridge-scrubbing:8080/alert'
       send_resolved: true
   ```

3. **Tune Detection Rules**:
   Edit `/prometheus/scrubbing-rules.yaml` to adjust thresholds:
   - Traffic rate threshold
   - Connection rate threshold
   - Attack duration

4. **Setup Upstream Scrubbing**:
   - Configure BGP peering with Yandex Cloud/Qrator
   - Test prefix announcements
   - Verify clean traffic forwarding

5. **Monitor in Production**:
   - Watch Grafana dashboard
   - Set up alerting notifications
   - Review attack patterns

## Troubleshooting

### Pods Not Starting

```bash
kubectl describe pod -n cloudbridge-scrubbing <pod-name>
kubectl logs -n cloudbridge-scrubbing <pod-name>
```

### GoBGP Connection Failed

Check that GoBGP is running:
```bash
kubectl get pods -n cloudbridge-dns-network -l app=gobgp
```

Update `GOBGP_ADDR` if service name differs:
```bash
kubectl edit deployment bgp-blackhole-exporter -n cloudbridge-scrubbing
```

### Metrics Not in Prometheus

Verify Prometheus targets:
```
http://<prometheus-url>/targets
```

Check NetworkPolicies allow scraping:
```bash
kubectl get networkpolicies -n cloudbridge-scrubbing
```

## Full Documentation

See [DDOS_PROTECTION_GUIDE.md](DDOS_PROTECTION_GUIDE.md) for complete documentation.
