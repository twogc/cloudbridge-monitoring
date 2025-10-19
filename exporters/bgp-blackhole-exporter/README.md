# CloudBridge BGP Blackhole Controller

Kubernetes-native BGP blackhole route injection controller using GoBGP v4 API.

## Features

- **Automatic BGP blackhole injection** via GoBGP v4 API
- **Alertmanager webhook integration** for automated response
- **Automatic route withdrawal** after configurable duration
- **Multi-tenancy support** with tenant/PoP labels
- **RFC 7999 compliant** blackhole community (65535:666)
- **Prometheus metrics** for monitoring

## Architecture

```
Alertmanager → /alert webhook → BGP Blackhole Controller → GoBGP → BGP Network
```

## Metrics

### `cloudbridge_bgp_blackhole_injected_total`
Total number of BGP blackhole routes injected.

Labels: `tenant`, `pop`, `prefix`

### `cloudbridge_bgp_blackhole_withdrawn_total`
Total number of BGP blackhole routes withdrawn.

Labels: `tenant`, `pop`, `prefix`

### `cloudbridge_bgp_blackhole_active`
Number of currently active BGP blackhole routes.

Labels: `tenant`, `pop`

### `cloudbridge_bgp_blackhole_duration_seconds`
Duration histogram of blackhole routes.

Labels: `tenant`, `pop`

## Configuration

Environment variables:

- `GOBGP_ADDR`: GoBGP gRPC address (default: `gobgp.cloudbridge-dns-network:50051`)

## API Endpoints

- `GET /metrics`: Prometheus metrics
- `GET /health`: Health check (verifies GoBGP connection)
- `POST /alert`: Alertmanager webhook

## Alertmanager Integration

### Alert Format

Alerts must include these labels:
- `tenant`: Tenant identifier
- `pop`: Point of Presence identifier
- `target_ip`: IP address to blackhole

Example Alertmanager configuration:

```yaml
receivers:
- name: 'bgp-blackhole'
  webhook_configs:
  - url: 'http://bgp-blackhole-exporter.cloudbridge-monitoring:8080/alert'
    send_resolved: true
```

### Behavior

- **Firing alert**: Injects blackhole route for `target_ip/32` with 1-hour TTL
- **Resolved alert**: Immediately withdraws blackhole route

## BGP Blackhole Community

Uses RFC 7999 blackhole community: **65535:666**

This community signals upstream BGP peers to drop traffic to the prefix.

## Building

```bash
go build -o bgp-blackhole-exporter .
```

## Running

```bash
export GOBGP_ADDR="gobgp-service:50051"
./bgp-blackhole-exporter
```

## Docker

```bash
docker build -t cloudbridge/bgp-blackhole-exporter:latest .
docker run -p 8080:8080 \
  -e GOBGP_ADDR="gobgp:50051" \
  cloudbridge/bgp-blackhole-exporter:latest
```

## Dependencies

- **GoBGP v4**: github.com/osrg/gobgp/v4
- **Prometheus client**: github.com/prometheus/client_golang v1.23.2
- **gRPC**: google.golang.org/grpc v1.73.0

## Security Considerations

- Controller requires network access to GoBGP gRPC API (port 50051)
- Webhook endpoint should be protected by NetworkPolicies
- Consider rate limiting on `/alert` endpoint for production
- BGP sessions should use MD5 authentication

## Troubleshooting

### Connection to GoBGP fails

```bash
kubectl logs -n cloudbridge-monitoring deployment/bgp-blackhole-exporter
```

Check `GOBGP_ADDR` environment variable and network connectivity.

### Blackholes not appearing in BGP table

Verify GoBGP is configured to advertise blackhole routes:

```bash
kubectl exec -n cloudbridge-dns-network gobgp-pod -- gobgpcli global rib
```

### Alert webhook not triggering

Check Alertmanager logs and verify webhook URL is correct.
