/*
CloudBridge Monitoring - Flow Telemetry Exporter
Prometheus exporter for flow telemetry metrics
*/

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// FlowExporter exports flow metrics to Prometheus
type FlowExporter struct {
	logger     *logrus.Logger
	aggregator *FlowAggregator

	// Custom metrics
	ipTrafficBytes   *prometheus.GaugeVec
	ipTrafficPackets *prometheus.GaugeVec
	portTraffic      *prometheus.GaugeVec
	protoTraffic     *prometheus.GaugeVec
	anomalyDetected  *prometheus.GaugeVec
	attackPattern    *prometheus.GaugeVec
}

// NewFlowExporter creates a new flow exporter
func NewFlowExporter(aggregator *FlowAggregator) *FlowExporter {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	exporter := &FlowExporter{
		logger:     logger,
		aggregator: aggregator,
	}

	exporter.initMetrics()

	return exporter
}

// initMetrics initializes custom Prometheus metrics
func (fe *FlowExporter) initMetrics() {
	// IP traffic metrics
	fe.ipTrafficBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_ip_traffic_bytes",
			Help: "Traffic in bytes per IP address",
		},
		[]string{"ip", "direction"},
	)

	fe.ipTrafficPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_ip_traffic_packets",
			Help: "Traffic in packets per IP address",
		},
		[]string{"ip", "direction"},
	)

	// Port traffic metrics
	fe.portTraffic = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_port_traffic_bytes",
			Help: "Traffic in bytes per destination port",
		},
		[]string{"port", "protocol"},
	)

	// Protocol traffic metrics
	fe.protoTraffic = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_protocol_traffic_bytes",
			Help: "Traffic in bytes per protocol",
		},
		[]string{"protocol"},
	)

	// Anomaly detection
	fe.anomalyDetected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_anomaly_detected",
			Help: "Detected anomalies (1 = anomaly, 0 = normal)",
		},
		[]string{"ip", "type", "severity"},
	)

	// Attack pattern detection
	fe.attackPattern = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_attack_pattern_detected",
			Help: "Detected attack patterns (1 = attack, 0 = normal)",
		},
		[]string{"ip", "attack_type"},
	)

	prometheus.MustRegister(fe.ipTrafficBytes)
	prometheus.MustRegister(fe.ipTrafficPackets)
	prometheus.MustRegister(fe.portTraffic)
	prometheus.MustRegister(fe.protoTraffic)
	prometheus.MustRegister(fe.anomalyDetected)
	prometheus.MustRegister(fe.attackPattern)
}

// UpdateMetrics updates all Prometheus metrics
func (fe *FlowExporter) UpdateMetrics() {
	fe.updateIPMetrics()
	fe.updatePortMetrics()
	fe.updateProtocolMetrics()
	fe.updateAnomalyMetrics()
	fe.updateAttackMetrics()
}

// updateIPMetrics updates IP traffic metrics
func (fe *FlowExporter) updateIPMetrics() {
	fe.aggregator.mu.RLock()
	defer fe.aggregator.mu.RUnlock()

	// Clear old metrics
	fe.ipTrafficBytes.Reset()
	fe.ipTrafficPackets.Reset()

	for ip, stats := range fe.aggregator.trafficByIP {
		// Sent traffic
		fe.ipTrafficBytes.WithLabelValues(ip, "sent").Set(float64(stats.BytesSent))
		fe.ipTrafficPackets.WithLabelValues(ip, "sent").Set(float64(stats.PacketsSent))

		// Received traffic
		fe.ipTrafficBytes.WithLabelValues(ip, "received").Set(float64(stats.BytesReceived))
		fe.ipTrafficPackets.WithLabelValues(ip, "received").Set(float64(stats.PacketsReceived))
	}
}

// updatePortMetrics updates port traffic metrics
func (fe *FlowExporter) updatePortMetrics() {
	fe.aggregator.mu.RLock()
	defer fe.aggregator.mu.RUnlock()

	fe.portTraffic.Reset()

	for port, stats := range fe.aggregator.trafficByPort {
		portStr := fmt.Sprintf("%d", port)
		protoName := getPortProtocol(port)
		fe.portTraffic.WithLabelValues(portStr, protoName).Set(float64(stats.Bytes))
	}
}

// updateProtocolMetrics updates protocol traffic metrics
func (fe *FlowExporter) updateProtocolMetrics() {
	fe.aggregator.mu.RLock()
	defer fe.aggregator.mu.RUnlock()

	fe.protoTraffic.Reset()

	for proto, stats := range fe.aggregator.trafficByProto {
		protoName := protocolName(proto)
		fe.protoTraffic.WithLabelValues(protoName).Set(float64(stats.Bytes))
	}
}

// updateAnomalyMetrics updates anomaly detection metrics
func (fe *FlowExporter) updateAnomalyMetrics() {
	// Detect anomalies with 3 sigma threshold
	anomalies := fe.aggregator.DetectAnomalies(3.0)

	// Reset metrics
	fe.anomalyDetected.Reset()

	// Set metrics for detected anomalies
	for _, anomaly := range anomalies {
		fe.anomalyDetected.WithLabelValues(
			anomaly.IP,
			anomaly.Type,
			anomaly.Severity,
		).Set(1)

		fe.logger.Warnf("Anomaly detected: IP=%s, Type=%s, Severity=%s, ZScore=%.2f",
			anomaly.IP, anomaly.Type, anomaly.Severity, anomaly.ZScore)
	}
}

// updateAttackMetrics updates attack pattern detection metrics
func (fe *FlowExporter) updateAttackMetrics() {
	fe.aggregator.mu.RLock()
	defer fe.aggregator.mu.RUnlock()

	fe.attackPattern.Reset()

	for ip := range fe.aggregator.trafficByIP {
		isAttack, attackType := fe.aggregator.IsAttackPattern(ip)
		if isAttack {
			fe.attackPattern.WithLabelValues(ip, attackType).Set(1)
			fe.logger.Warnf("Attack pattern detected: IP=%s, Type=%s", ip, attackType)
		}
	}
}

// ServeMetrics starts the Prometheus metrics HTTP server
func (fe *FlowExporter) ServeMetrics(addr string) error {
	fe.logger.Infof("Starting Prometheus metrics server on %s", addr)

	// Update metrics periodically
	go fe.metricsUpdateLoop()

	// Serve Prometheus metrics
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", fe.handleHealth)
	http.HandleFunc("/api/top-talkers", fe.handleTopTalkers)
	http.HandleFunc("/api/ip-stats", fe.handleIPStats)
	http.HandleFunc("/api/anomalies", fe.handleAnomalies)

	return http.ListenAndServe(addr, nil)
}

// metricsUpdateLoop periodically updates metrics
func (fe *FlowExporter) metricsUpdateLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fe.UpdateMetrics()
	}
}

// handleHealth handles health check requests
func (fe *FlowExporter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

// handleTopTalkers handles top talkers API requests
func (fe *FlowExporter) handleTopTalkers(w http.ResponseWriter, r *http.Request) {
	sources, destinations := fe.aggregator.GetTopTalkers()

	response := map[string]interface{}{
		"sources":      sources,
		"destinations": destinations,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleIPStats handles IP statistics API requests
func (fe *FlowExporter) handleIPStats(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		http.Error(w, "missing ip parameter", http.StatusBadRequest)
		return
	}

	stats, err := fe.aggregator.GetIPStats(ip)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAnomalies handles anomaly detection API requests
func (fe *FlowExporter) handleAnomalies(w http.ResponseWriter, r *http.Request) {
	anomalies := fe.aggregator.DetectAnomalies(3.0)

	response := map[string]interface{}{
		"count":     len(anomalies),
		"anomalies": anomalies,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getPortProtocol returns likely protocol for common ports
func getPortProtocol(port uint16) string {
	switch port {
	case 80, 443, 8080, 8443:
		return "http"
	case 53:
		return "dns"
	case 22:
		return "ssh"
	case 25, 587, 465:
		return "smtp"
	case 3306:
		return "mysql"
	case 5432:
		return "postgresql"
	case 6379:
		return "redis"
	case 27017:
		return "mongodb"
	default:
		if port < 1024 {
			return "system"
		}
		return "other"
	}
}
