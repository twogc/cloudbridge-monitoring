/*
CloudBridge Monitoring - FastNetMon Notification Handler
Handles attack notifications from FastNetMon via webhooks and scripts
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// NotificationHandler handles FastNetMon attack notifications
type NotificationHandler struct {
	logger *logrus.Logger

	// Mitigation coordinator
	mitigationCoordinator MitigationCoordinator

	// Active mitigations
	activeMitigations map[string]*MitigationInfo
	mu                sync.RWMutex

	// Metrics
	notificationsReceived *prometheus.CounterVec
	mitigationsTriggered  prometheus.Counter
	mitigationsFailed     prometheus.Counter
	mitigationDuration    prometheus.Histogram

	// Router
	router *mux.Router
}

// MitigationCoordinator coordinates mitigation actions
type MitigationCoordinator interface {
	// TriggerMitigation triggers mitigation for an attack
	TriggerMitigation(ctx context.Context, attack *AttackInfo) (*MitigationInfo, error)

	// StopMitigation stops mitigation
	StopMitigation(ctx context.Context, mitigationID string) error

	// GetMitigationStatus gets mitigation status
	GetMitigationStatus(ctx context.Context, mitigationID string) (*MitigationInfo, error)
}

// MitigationInfo represents information about active mitigation
type MitigationInfo struct {
	ID               string
	AttackID         string
	TargetIP         string
	Strategy         string    // local, rtbh, scrubbing, hybrid
	Status           string    // pending, active, completed, failed
	StartedAt        time.Time
	CompletedAt      *time.Time
	MitigationMethod string
	Metadata         map[string]interface{}
}

// AttackNotification represents a FastNetMon attack notification
type AttackNotification struct {
	Type      string    `json:"type"`       // attack_started, attack_stopped
	AttackID  string    `json:"attack_id"`
	TargetIP  string    `json:"target_ip"`
	Timestamp time.Time `json:"timestamp"`

	// Attack details
	AttackType   string  `json:"attack_type"`
	AttackVector string  `json:"attack_vector"`
	Severity     string  `json:"severity"`
	Confidence   float64 `json:"confidence"`

	// Traffic metrics
	TotalTraffic uint64 `json:"total_traffic"`
	TotalPackets uint64 `json:"total_packets"`
	PeakTraffic  uint64 `json:"peak_traffic"`
	PeakPackets  uint64 `json:"peak_packets"`

	// Protocol breakdown
	TCPTraffic  uint64 `json:"tcp_traffic"`
	UDPTraffic  uint64 `json:"udp_traffic"`
	ICMPTraffic uint64 `json:"icmp_traffic"`

	// Source information
	SourceIPs   []string `json:"source_ips,omitempty"`
	SourceASNs  []uint32 `json:"source_asns,omitempty"`
	SourcePorts []uint16 `json:"source_ports,omitempty"`

	// Metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// NewNotificationHandler creates a new notification handler
func NewNotificationHandler(coordinator MitigationCoordinator) *NotificationHandler {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	nh := &NotificationHandler{
		logger:                logger,
		mitigationCoordinator: coordinator,
		activeMitigations:     make(map[string]*MitigationInfo),
		router:                mux.NewRouter(),
	}

	// Initialize metrics
	nh.initMetrics()

	// Setup routes
	nh.setupRoutes()

	return nh
}

// initMetrics initializes Prometheus metrics
func (nh *NotificationHandler) initMetrics() {
	nh.notificationsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_fastnetmon_notifications_received_total",
			Help: "Total number of notifications received from FastNetMon",
		},
		[]string{"type", "severity"},
	)

	nh.mitigationsTriggered = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_fastnetmon_mitigations_triggered_total",
		Help: "Total number of mitigations triggered",
	})

	nh.mitigationsFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_fastnetmon_mitigations_failed_total",
		Help: "Total number of failed mitigations",
	})

	nh.mitigationDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cloudbridge_fastnetmon_mitigation_duration_seconds",
		Help:    "Duration of mitigation actions",
		Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
	})

	prometheus.MustRegister(nh.notificationsReceived)
	prometheus.MustRegister(nh.mitigationsTriggered)
	prometheus.MustRegister(nh.mitigationsFailed)
	prometheus.MustRegister(nh.mitigationDuration)
}

// setupRoutes sets up HTTP routes
func (nh *NotificationHandler) setupRoutes() {
	// Webhook endpoints
	nh.router.HandleFunc("/webhook/attack", nh.handleAttackWebhook).Methods("POST")
	nh.router.HandleFunc("/webhook/attack/started", nh.handleAttackStarted).Methods("POST")
	nh.router.HandleFunc("/webhook/attack/stopped", nh.handleAttackStopped).Methods("POST")

	// API endpoints
	nh.router.HandleFunc("/api/mitigations", nh.handleListMitigations).Methods("GET")
	nh.router.HandleFunc("/api/mitigations/{id}", nh.handleGetMitigation).Methods("GET")
	nh.router.HandleFunc("/api/mitigations/{id}/stop", nh.handleStopMitigation).Methods("POST")

	// Health and metrics
	nh.router.HandleFunc("/health", nh.handleHealth).Methods("GET")
	nh.router.Handle("/metrics", promhttp.Handler())
}

// handleAttackWebhook handles generic attack webhooks
func (nh *NotificationHandler) handleAttackWebhook(w http.ResponseWriter, r *http.Request) {
	var notification AttackNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Record notification
	nh.notificationsReceived.WithLabelValues(notification.Type, notification.Severity).Inc()

	nh.logger.Infof("Received %s notification for %s (severity: %s)",
		notification.Type, notification.TargetIP, notification.Severity)

	// Route based on type
	switch notification.Type {
	case "attack_started":
		if err := nh.processAttackStarted(&notification); err != nil {
			nh.logger.Errorf("Failed to process attack_started: %v", err)
			nh.mitigationsFailed.Inc()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case "attack_stopped":
		if err := nh.processAttackStopped(&notification); err != nil {
			nh.logger.Errorf("Failed to process attack_stopped: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	default:
		nh.logger.Warnf("Unknown notification type: %s", notification.Type)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "notification processed",
	})
}

// handleAttackStarted handles attack started notifications
func (nh *NotificationHandler) handleAttackStarted(w http.ResponseWriter, r *http.Request) {
	var notification AttackNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	notification.Type = "attack_started"
	nh.notificationsReceived.WithLabelValues("attack_started", notification.Severity).Inc()

	if err := nh.processAttackStarted(&notification); err != nil {
		nh.logger.Errorf("Failed to process attack_started: %v", err)
		nh.mitigationsFailed.Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleAttackStopped handles attack stopped notifications
func (nh *NotificationHandler) handleAttackStopped(w http.ResponseWriter, r *http.Request) {
	var notification AttackNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	notification.Type = "attack_stopped"
	nh.notificationsReceived.WithLabelValues("attack_stopped", notification.Severity).Inc()

	if err := nh.processAttackStopped(&notification); err != nil {
		nh.logger.Errorf("Failed to process attack_stopped: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// processAttackStarted processes attack started event
func (nh *NotificationHandler) processAttackStarted(notification *AttackNotification) error {
	nh.logger.Infof("Processing attack started: %s targeting %s (severity: %s)",
		notification.AttackID, notification.TargetIP, notification.Severity)

	// Convert notification to AttackInfo
	attack := &AttackInfo{
		AttackID:     notification.AttackID,
		TargetIP:     notification.TargetIP,
		AttackType:   notification.AttackType,
		AttackVector: notification.AttackVector,
		StartTime:    notification.Timestamp,
		TotalTraffic: notification.TotalTraffic,
		TotalPackets: notification.TotalPackets,
		PeakTraffic:  notification.PeakTraffic,
		PeakPackets:  notification.PeakPackets,
		TCPTraffic:   notification.TCPTraffic,
		UDPTraffic:   notification.UDPTraffic,
		ICMPTraffic:  notification.ICMPTraffic,
		Severity:     notification.Severity,
		Confidence:   notification.Confidence,
		SourceIPs:    notification.SourceIPs,
		SourceASNs:   notification.SourceASNs,
		SourcePorts:  notification.SourcePorts,
		Metadata:     notification.Metadata,
	}

	// Trigger mitigation
	ctx := context.Background()
	mitigation, err := nh.mitigationCoordinator.TriggerMitigation(ctx, attack)
	if err != nil {
		return fmt.Errorf("failed to trigger mitigation: %w", err)
	}

	// Store active mitigation
	nh.mu.Lock()
	nh.activeMitigations[notification.AttackID] = mitigation
	nh.mu.Unlock()

	nh.mitigationsTriggered.Inc()
	nh.logger.Infof("Triggered mitigation %s for attack %s", mitigation.ID, notification.AttackID)

	return nil
}

// processAttackStopped processes attack stopped event
func (nh *NotificationHandler) processAttackStopped(notification *AttackNotification) error {
	nh.logger.Infof("Processing attack stopped: %s targeting %s",
		notification.AttackID, notification.TargetIP)

	// Get mitigation info
	nh.mu.RLock()
	mitigation, exists := nh.activeMitigations[notification.AttackID]
	nh.mu.RUnlock()

	if !exists {
		nh.logger.Warnf("No active mitigation found for attack %s", notification.AttackID)
		return nil
	}

	// Stop mitigation
	ctx := context.Background()
	if err := nh.mitigationCoordinator.StopMitigation(ctx, mitigation.ID); err != nil {
		return fmt.Errorf("failed to stop mitigation: %w", err)
	}

	// Calculate duration
	duration := time.Since(mitigation.StartedAt).Seconds()
	nh.mitigationDuration.Observe(duration)

	// Remove from active mitigations
	nh.mu.Lock()
	delete(nh.activeMitigations, notification.AttackID)
	nh.mu.Unlock()

	nh.logger.Infof("Stopped mitigation %s for attack %s (duration: %.2fs)",
		mitigation.ID, notification.AttackID, duration)

	return nil
}

// handleListMitigations handles GET /api/mitigations
func (nh *NotificationHandler) handleListMitigations(w http.ResponseWriter, r *http.Request) {
	nh.mu.RLock()
	defer nh.mu.RUnlock()

	mitigations := make([]*MitigationInfo, 0, len(nh.activeMitigations))
	for _, m := range nh.activeMitigations {
		mitigations = append(mitigations, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":       len(mitigations),
		"mitigations": mitigations,
	})
}

// handleGetMitigation handles GET /api/mitigations/{id}
func (nh *NotificationHandler) handleGetMitigation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	mitigationID := vars["id"]

	// Try to find in active mitigations first
	nh.mu.RLock()
	for _, m := range nh.activeMitigations {
		if m.ID == mitigationID {
			nh.mu.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(m)
			return
		}
	}
	nh.mu.RUnlock()

	// Query coordinator
	ctx := context.Background()
	mitigation, err := nh.mitigationCoordinator.GetMitigationStatus(ctx, mitigationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(mitigation)
}

// handleStopMitigation handles POST /api/mitigations/{id}/stop
func (nh *NotificationHandler) handleStopMitigation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	mitigationID := vars["id"]

	ctx := context.Background()
	if err := nh.mitigationCoordinator.StopMitigation(ctx, mitigationID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "mitigation stopped",
	})
}

// handleHealth handles GET /health
func (nh *NotificationHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// Start starts the notification handler HTTP server
func (nh *NotificationHandler) Start(addr string) error {
	nh.logger.Infof("Starting notification handler on %s", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      nh.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return server.ListenAndServe()
}

// StartWithContext starts the notification handler with context
func (nh *NotificationHandler) StartWithContext(ctx context.Context, addr string) error {
	nh.logger.Infof("Starting notification handler on %s", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      nh.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			nh.logger.Errorf("Notification handler error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	nh.logger.Info("Shutting down notification handler...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}
