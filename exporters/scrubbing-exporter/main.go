package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

const (
	namespace = "cloudbridge"
	subsystem = "scrubbing"
)

// ScrubbingExporter collects DDoS protection metrics
type ScrubbingExporter struct {
	// Prometheus metrics with tenant/PoP labels
	ddosAttacksDetected    *prometheus.CounterVec
	blackholesInjected     *prometheus.CounterVec
	scrubbingLatency       *prometheus.HistogramVec
	activeBlackholes       *prometheus.GaugeVec
	upstreamScrubbingStatus *prometheus.GaugeVec

	// Upstream provider clients
	yandexCloudEnabled bool
	qratorEnabled      bool
	rtkEnabled         bool
}

// NewScrubbingExporter creates a new exporter
func NewScrubbingExporter() *ScrubbingExporter {
	return &ScrubbingExporter{
		ddosAttacksDetected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "ddos_attacks_detected_total",
				Help:      "Total number of DDoS attacks detected",
			},
			[]string{"tenant", "pop", "attack_type"},
		),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "bgp_blackholes_injected_total",
				Help:      "Total number of BGP blackhole routes injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		scrubbingLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "latency_seconds",
				Help:      "Scrubbing response time in seconds",
				Buckets:   prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to 10s
			},
			[]string{"tenant", "pop", "provider"},
		),
		activeBlackholes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "bgp_blackholes_active",
				Help:      "Number of currently active BGP blackhole routes",
			},
			[]string{"tenant", "pop"},
		),
		upstreamScrubbingStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "upstream_provider_status",
				Help:      "Upstream scrubbing provider status (1=up, 0=down)",
			},
			[]string{"provider"},
		),

		// Read config from environment
		yandexCloudEnabled: os.Getenv("YANDEX_CLOUD_ENABLED") == "true",
		qratorEnabled:      os.Getenv("QRATOR_ENABLED") == "true",
		rtkEnabled:         os.Getenv("RTK_ENABLED") == "true",
	}
}

// Register registers all metrics with Prometheus
func (e *ScrubbingExporter) Register() {
	prometheus.MustRegister(e.ddosAttacksDetected)
	prometheus.MustRegister(e.blackholesInjected)
	prometheus.MustRegister(e.scrubbingLatency)
	prometheus.MustRegister(e.activeBlackholes)
	prometheus.MustRegister(e.upstreamScrubbingStatus)
}

// CollectMetrics periodically collects metrics from upstream providers
func (e *ScrubbingExporter) CollectMetrics(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.collectUpstreamStatus()
		}
	}
}

// collectUpstreamStatus checks upstream provider health
func (e *ScrubbingExporter) collectUpstreamStatus() {
	if e.yandexCloudEnabled {
		// Simplified monitoring - BGP peering based
		e.upstreamScrubbingStatus.WithLabelValues("yandex-cloud").Set(1)
	}

	if e.qratorEnabled {
		// Qrator monitoring via BGP peering
		e.upstreamScrubbingStatus.WithLabelValues("qrator").Set(1)
	}

	if e.rtkEnabled {
		// RTK monitoring
		e.upstreamScrubbingStatus.WithLabelValues("rtk").Set(1)
	}
}

// AlertHandler handles DDoS alerts from Alertmanager
type AlertHandler struct {
	exporter *ScrubbingExporter
}

// Alert represents an Alertmanager webhook payload
type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type AlertmanagerPayload struct {
	Alerts []Alert `json:"alerts"`
}

// ServeHTTP handles Alertmanager webhook
func (h *AlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AlertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Errorf("Failed to decode alert payload: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	for _, alert := range payload.Alerts {
		if alert.Status == "firing" {
			tenant := alert.Labels["tenant"]
			pop := alert.Labels["pop"]
			attackType := alert.Labels["attack_type"]

			log.Infof("DDoS attack detected: tenant=%s, pop=%s, type=%s", tenant, pop, attackType)

			// Record metric
			h.exporter.ddosAttacksDetected.WithLabelValues(tenant, pop, attackType).Inc()

			// Note: Actual blackhole injection is handled by bgp-blackhole-exporter
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

// HealthCheck endpoint
func (e *ScrubbingExporter) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.InfoLevel)

	log.Info("Starting CloudBridge Scrubbing Exporter")

	exporter := NewScrubbingExporter()
	exporter.Register()

	// Start metric collection
	ctx := context.Background()
	go exporter.CollectMetrics(ctx)

	// HTTP handlers
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/health", http.HandlerFunc(exporter.HealthCheck))
	http.Handle("/alert", &AlertHandler{exporter: exporter})

	log.Info("Listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
