#!/bin/bash
#
# CloudBridge DDoS Protection Deployment Script
#
# Deploys scrubbing-exporter and bgp-blackhole-exporter with full monitoring stack
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "🚀 CloudBridge DDoS Protection Deployment"
echo "=========================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Helper functions
success() {
    echo -e "${GREEN}✓${NC} $1"
}

warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
}

# Check prerequisites
echo "📋 Checking prerequisites..."

if ! command -v kubectl &> /dev/null; then
    error "kubectl not found. Please install kubectl."
    exit 1
fi
success "kubectl found"

if ! command -v docker &> /dev/null; then
    warning "docker not found. Skipping image build."
    SKIP_BUILD=true
else
    success "docker found"
    SKIP_BUILD=false
fi

echo ""

# Step 1: Create namespace and RBAC
echo "1️⃣  Creating cloudbridge-scrubbing namespace..."
kubectl apply -f "${PROJECT_ROOT}/security/scrubbing-namespace.yaml"
success "Namespace created"

echo "2️⃣  Applying RBAC policies..."
kubectl apply -f "${PROJECT_ROOT}/security/scrubbing-rbac.yaml"
success "RBAC configured"

echo "3️⃣  Applying NetworkPolicies..."
kubectl apply -f "${PROJECT_ROOT}/security/scrubbing-network-policy.yaml"
success "NetworkPolicies applied"

echo ""

# Step 2: Build and push Docker images (optional)
if [ "$SKIP_BUILD" = false ]; then
    echo "4️⃣  Building Docker images..."

    # Build scrubbing-exporter
    echo "   Building scrubbing-exporter..."
    docker build -t cloudbridge/scrubbing-exporter:latest \
        "${PROJECT_ROOT}/exporters/scrubbing-exporter"
    success "scrubbing-exporter image built"

    # Build bgp-blackhole-exporter
    echo "   Building bgp-blackhole-exporter..."
    docker build -t cloudbridge/bgp-blackhole-exporter:latest \
        "${PROJECT_ROOT}/exporters/bgp-blackhole-exporter"
    success "bgp-blackhole-exporter image built"

    echo ""
    warning "Don't forget to push images to your registry:"
    echo "   docker push cloudbridge/scrubbing-exporter:latest"
    echo "   docker push cloudbridge/bgp-blackhole-exporter:latest"
    echo ""
else
    warning "Skipping Docker build (docker not found)"
    echo ""
fi

# Step 3: Deploy exporters
echo "5️⃣  Deploying scrubbing-exporter..."
kubectl apply -f "${PROJECT_ROOT}/exporters/scrubbing-exporter/k8s/deployment.yaml"
success "scrubbing-exporter deployed"

echo "6️⃣  Deploying bgp-blackhole-exporter..."
kubectl apply -f "${PROJECT_ROOT}/exporters/bgp-blackhole-exporter/k8s/deployment.yaml"
success "bgp-blackhole-exporter deployed"

echo ""

# Step 4: Configure Prometheus
echo "7️⃣  Configuring Prometheus scraping..."
kubectl apply -f "${PROJECT_ROOT}/prometheus/scrubbing-scrape-config.yaml"
success "Prometheus scrape config applied"

echo "8️⃣  Configuring Prometheus alerting rules..."
kubectl apply -f "${PROJECT_ROOT}/prometheus/scrubbing-rules.yaml"
success "Prometheus alerting rules applied"

echo ""

# Step 5: Import Grafana dashboard
echo "9️⃣  Importing Grafana dashboard..."
kubectl create configmap grafana-dashboard-scrubbing \
    --from-file="${PROJECT_ROOT}/grafana/scrubbing-dashboard.json" \
    -n cloudbridge-monitoring \
    --dry-run=client -o yaml | kubectl apply -f -
success "Grafana dashboard imported"

echo ""

# Step 6: Wait for deployments
echo "⏳ Waiting for deployments to be ready..."
kubectl wait --for=condition=available --timeout=300s \
    deployment/scrubbing-exporter \
    -n cloudbridge-scrubbing
success "scrubbing-exporter ready"

kubectl wait --for=condition=available --timeout=300s \
    deployment/bgp-blackhole-exporter \
    -n cloudbridge-scrubbing
success "bgp-blackhole-exporter ready"

echo ""

# Step 7: Verify deployment
echo "✅ Verifying deployment..."

# Check pods
SCRUBBING_PODS=$(kubectl get pods -n cloudbridge-scrubbing -l app=scrubbing-exporter --no-headers | wc -l)
BLACKHOLE_PODS=$(kubectl get pods -n cloudbridge-scrubbing -l app=bgp-blackhole-exporter --no-headers | wc -l)

if [ "$SCRUBBING_PODS" -ge 1 ]; then
    success "scrubbing-exporter pods running: $SCRUBBING_PODS"
else
    error "No scrubbing-exporter pods running"
fi

if [ "$BLACKHOLE_PODS" -ge 1 ]; then
    success "bgp-blackhole-exporter pods running: $BLACKHOLE_PODS"
else
    error "No bgp-blackhole-exporter pods running"
fi

echo ""

# Step 8: Display access information
echo "=========================================="
echo "🎉 Deployment Complete!"
echo "=========================================="
echo ""
echo "📊 Monitoring URLs:"
echo "   Grafana Dashboard: http://<grafana-url>/d/scrubbing"
echo "   Prometheus: http://<prometheus-url>"
echo ""
echo "🔍 Useful commands:"
echo "   # Check scrubbing exporter logs"
echo "   kubectl logs -n cloudbridge-scrubbing -l app=scrubbing-exporter -f"
echo ""
echo "   # Check blackhole controller logs"
echo "   kubectl logs -n cloudbridge-scrubbing -l app=bgp-blackhole-exporter -f"
echo ""
echo "   # View active blackholes"
echo "   kubectl exec -n cloudbridge-dns-network <gobgp-pod> -- gobgpcli global rib"
echo ""
echo "   # Test scrubbing exporter health"
echo "   kubectl exec -n cloudbridge-scrubbing deployment/scrubbing-exporter -- curl -s http://localhost:8080/health"
echo ""
echo "   # Test blackhole controller health"
echo "   kubectl exec -n cloudbridge-scrubbing deployment/bgp-blackhole-exporter -- curl -s http://localhost:8080/health"
echo ""
echo "⚠️  Next steps:"
echo "   1. Configure API keys in cloudbridge-scrubbing/scrubbing-secrets Secret"
echo "   2. Update Alertmanager webhook URLs if needed"
echo "   3. Test DDoS detection and blackhole injection"
echo "   4. Monitor Grafana dashboard for metrics"
echo ""
echo "📚 Documentation: ${PROJECT_ROOT}/docs/DDOS_PROTECTION_GUIDE.md"
echo ""
