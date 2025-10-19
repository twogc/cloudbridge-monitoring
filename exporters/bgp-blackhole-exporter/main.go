package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	namespace = "cloudbridge"
	subsystem = "bgp_blackhole"

	// BGP community for blackhole (RFC 7999)
	blackholeCommunity = "65535:666"
)

// BGPBlackholeController manages BGP blackhole route injection
type BGPBlackholeController struct {
	gobgpClient gobgpapi.GobgpApiClient
	conn        *grpc.ClientConn
	gobgpAddr   string

	// Active blackholes tracking
	mu               sync.RWMutex
	activeBlackholes map[string]*BlackholeEntry

	// Prometheus metrics
	blackholesInjected   *prometheus.CounterVec
	blackholesWithdrawn  *prometheus.CounterVec
	activeBlackholesGauge *prometheus.GaugeVec
	blackholeDuration    *prometheus.HistogramVec
}

// BlackholeEntry represents an active blackhole route
type BlackholeEntry struct {
	Prefix      string
	Tenant      string
	PoP         string
	InjectedAt  time.Time
	ExpiresAt   time.Time
	CancelTimer *time.Timer
}

// NewBGPBlackholeController creates a new controller
func NewBGPBlackholeController(gobgpAddr string) (*BGPBlackholeController, error) {
	conn, err := grpc.NewClient(
		gobgpAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to GoBGP: %w", err)
	}

	return &BGPBlackholeController{
		gobgpClient:      gobgpapi.NewGobgpApiClient(conn),
		conn:             conn,
		gobgpAddr:        gobgpAddr,
		activeBlackholes: make(map[string]*BlackholeEntry),

		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "injected_total",
				Help:      "Total number of BGP blackhole routes injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		blackholesWithdrawn: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "withdrawn_total",
				Help:      "Total number of BGP blackhole routes withdrawn",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "active",
				Help:      "Number of currently active BGP blackhole routes",
			},
			[]string{"tenant", "pop"},
		),
		blackholeDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "duration_seconds",
				Help:      "Duration of blackhole routes in seconds",
				Buckets:   prometheus.ExponentialBuckets(60, 2, 10), // 1min to 8.5 hours
			},
			[]string{"tenant", "pop"},
		),
	}, nil
}

// Register registers metrics with Prometheus
func (c *BGPBlackholeController) Register() {
	prometheus.MustRegister(c.blackholesInjected)
	prometheus.MustRegister(c.blackholesWithdrawn)
	prometheus.MustRegister(c.activeBlackholesGauge)
	prometheus.MustRegister(c.blackholeDuration)
}

// InjectBlackhole injects a BGP blackhole route
func (c *BGPBlackholeController) InjectBlackhole(prefix, tenant, pop string, duration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if existing, exists := c.activeBlackholes[prefix]; exists {
		log.Warnf("Blackhole already exists for %s, extending duration", prefix)
		existing.CancelTimer.Stop()
	}

	// Parse prefix
	ip, ipNet, err := net.ParseCIDR(prefix)
	if err != nil {
		return fmt.Errorf("invalid prefix %s: %w", prefix, err)
	}

	prefixLen, _ := ipNet.Mask.Size()

	// Create BGP path with blackhole community
	nlri, err := anypb.New(&gobgpapi.IPAddressPrefix{
		Prefix:    ip.String(),
		PrefixLen: uint32(prefixLen),
	})
	if err != nil {
		return fmt.Errorf("failed to create NLRI: %w", err)
	}

	// Add blackhole community (65535:666)
	community, err := anypb.New(&gobgpapi.CommunitiesAttribute{
		Communities: []uint32{0xFFFF029A}, // 65535:666 in 32-bit format
	})
	if err != nil {
		return fmt.Errorf("failed to create community: %w", err)
	}

	// Create path
	path := &gobgpapi.Path{
		Family: &gobgpapi.Family{
			Afi:  gobgpapi.Family_AFI_IP,
			Safi: gobgpapi.Family_SAFI_UNICAST,
		},
		Nlri:   nlri,
		Pattrs: []*anypb.Any{community},
	}

	// Inject path via GoBGP API
	_, err = c.gobgpClient.AddPath(context.Background(), &gobgpapi.AddPathRequest{
		Path: path,
	})
	if err != nil {
		return fmt.Errorf("failed to inject blackhole for %s: %w", prefix, err)
	}

	log.Infof("Injected blackhole: prefix=%s, tenant=%s, pop=%s, duration=%s", prefix, tenant, pop, duration)

	// Schedule automatic withdrawal
	timer := time.AfterFunc(duration, func() {
		if err := c.WithdrawBlackhole(prefix, tenant, pop); err != nil {
			log.Errorf("Failed to auto-withdraw blackhole %s: %v", prefix, err)
		}
	})

	// Track active blackhole
	c.activeBlackholes[prefix] = &BlackholeEntry{
		Prefix:      prefix,
		Tenant:      tenant,
		PoP:         pop,
		InjectedAt:  time.Now(),
		ExpiresAt:   time.Now().Add(duration),
		CancelTimer: timer,
	}

	// Update metrics
	c.blackholesInjected.WithLabelValues(tenant, pop, prefix).Inc()
	c.activeBlackholesGauge.WithLabelValues(tenant, pop).Inc()

	return nil
}

// WithdrawBlackhole withdraws a BGP blackhole route
func (c *BGPBlackholeController) WithdrawBlackhole(prefix, tenant, pop string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.activeBlackholes[prefix]
	if !exists {
		return fmt.Errorf("blackhole not found for prefix %s", prefix)
	}

	// Parse prefix
	ip, ipNet, err := net.ParseCIDR(prefix)
	if err != nil {
		return fmt.Errorf("invalid prefix %s: %w", prefix, err)
	}

	prefixLen, _ := ipNet.Mask.Size()

	// Create NLRI for withdrawal
	nlri, err := anypb.New(&gobgpapi.IPAddressPrefix{
		Prefix:    ip.String(),
		PrefixLen: uint32(prefixLen),
	})
	if err != nil {
		return fmt.Errorf("failed to create NLRI: %w", err)
	}

	// Withdraw path
	path := &gobgpapi.Path{
		Family: &gobgpapi.Family{
			Afi:  gobgpapi.Family_AFI_IP,
			Safi: gobgpapi.Family_SAFI_UNICAST,
		},
		Nlri: nlri,
	}

	_, err = c.gobgpClient.DeletePath(context.Background(), &gobgpapi.DeletePathRequest{
		Path: path,
	})
	if err != nil {
		return fmt.Errorf("failed to withdraw blackhole for %s: %w", prefix, err)
	}

	log.Infof("Withdrew blackhole: prefix=%s, tenant=%s, pop=%s", prefix, tenant, pop)

	// Cancel timer and remove from tracking
	entry.CancelTimer.Stop()
	delete(c.activeBlackholes, prefix)

	// Update metrics
	duration := time.Since(entry.InjectedAt).Seconds()
	c.blackholesWithdrawn.WithLabelValues(tenant, pop, prefix).Inc()
	c.activeBlackholesGauge.WithLabelValues(tenant, pop).Dec()
	c.blackholeDuration.WithLabelValues(tenant, pop).Observe(duration)

	return nil
}

// AlertHandler handles Alertmanager webhooks
type AlertHandler struct {
	controller *BGPBlackholeController
}

// Alert represents Alertmanager alert
type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type AlertmanagerPayload struct {
	Alerts []Alert `json:"alerts"`
}

// ServeHTTP handles incoming alerts
func (h *AlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload AlertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Errorf("Failed to decode alert: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	for _, alert := range payload.Alerts {
		tenant := alert.Labels["tenant"]
		pop := alert.Labels["pop"]
		targetIP := alert.Labels["target_ip"]

		if targetIP == "" {
			log.Warn("Alert missing target_ip label, skipping")
			continue
		}

		prefix := fmt.Sprintf("%s/32", targetIP)

		if alert.Status == "firing" {
			// Inject blackhole
			duration := 1 * time.Hour // Default duration
			if err := h.controller.InjectBlackhole(prefix, tenant, pop, duration); err != nil {
				log.Errorf("Failed to inject blackhole: %v", err)
			}
		} else if alert.Status == "resolved" {
			// Withdraw blackhole
			if err := h.controller.WithdrawBlackhole(prefix, tenant, pop); err != nil {
				log.Errorf("Failed to withdraw blackhole: %v", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

// HealthCheck endpoint
func (c *BGPBlackholeController) HealthCheck(w http.ResponseWriter, r *http.Request) {
	// Check GoBGP connection
	_, err := c.gobgpClient.GetBgp(context.Background(), &gobgpapi.GetBgpRequest{})
	if err != nil {
		http.Error(w, fmt.Sprintf("GoBGP unhealthy: %v", err), http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.InfoLevel)

	log.Info("Starting CloudBridge BGP Blackhole Controller")

	// Get GoBGP address from environment
	gobgpAddr := os.Getenv("GOBGP_ADDR")
	if gobgpAddr == "" {
		gobgpAddr = "gobgp.cloudbridge-dns-network:50051" // Default
	}

	controller, err := NewBGPBlackholeController(gobgpAddr)
	if err != nil {
		log.Fatalf("Failed to create controller: %v", err)
	}
	defer controller.conn.Close()

	controller.Register()

	// HTTP handlers
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/health", http.HandlerFunc(controller.HealthCheck))
	http.Handle("/alert", &AlertHandler{controller: controller})

	log.Info("Listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
