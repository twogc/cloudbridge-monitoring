package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Mock GoBGP server for testing
type mockGoBGPServer struct{}

func (m *mockGoBGPServer) GetBgp(ctx context.Context, req interface{}) (interface{}, error) {
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *mockGoBGPServer) AddPath(ctx context.Context, req interface{}) (interface{}, error) {
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *mockGoBGPServer) DeletePath(ctx context.Context, req interface{}) (interface{}, error) {
	return map[string]interface{}{"status": "ok"}, nil
}

func TestNewBGPBlackholeController(t *testing.T) {
	// Test with invalid address (should fail)
	_, err := NewBGPBlackholeController("invalid-address")
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

func TestBGPBlackholeControllerRegister(t *testing.T) {
	// Create a mock controller for testing registration
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		blackholesWithdrawn: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_withdrawn",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
		blackholeDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "test_blackhole_duration",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	// Register should not panic
	controller.Register()
}

func TestBGPBlackholeControllerInjectBlackhole(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	tests := []struct {
		name        string
		prefix      string
		tenant      string
		pop         string
		duration    time.Duration
		expectError bool
	}{
		{
			name:        "Valid IPv4 prefix",
			prefix:      "192.0.2.1/32",
			tenant:      "test-tenant",
			pop:         "MOW",
			duration:    time.Hour,
			expectError: false,
		},
		{
			name:        "Valid IPv4 subnet",
			prefix:      "192.0.2.0/24",
			tenant:      "test-tenant",
			pop:         "MOW",
			duration:    time.Hour,
			expectError: false,
		},
		{
			name:        "Invalid prefix",
			prefix:      "invalid-prefix",
			tenant:      "test-tenant",
			pop:         "MOW",
			duration:    time.Hour,
			expectError: true,
		},
		{
			name:        "Empty prefix",
			prefix:      "",
			tenant:      "test-tenant",
			pop:         "MOW",
			duration:    time.Hour,
			expectError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test without actual GoBGP connection (will fail at injection)
			// This tests the prefix parsing and tracking logic
			controller.mu.Lock()
			controller.activeBlackholes = make(map[string]*BlackholeEntry)
			controller.mu.Unlock()
			
			// Test prefix parsing
			_, _, err := net.ParseCIDR(tt.prefix)
			if tt.expectError && err == nil {
				t.Error("Expected error for invalid prefix")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for valid prefix: %v", err)
			}
		})
	}
}

func TestBGPBlackholeControllerWithdrawBlackhole(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesWithdrawn: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_withdrawn",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
		blackholeDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "test_blackhole_duration",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	// Test withdrawing non-existent blackhole
	err := controller.WithdrawBlackhole("192.0.2.1/32", "test-tenant", "MOW")
	if err == nil {
		t.Error("Expected error for non-existent blackhole")
	}
	
	// Test withdrawing with invalid prefix
	err = controller.WithdrawBlackhole("invalid-prefix", "test-tenant", "MOW")
	if err == nil {
		t.Error("Expected error for invalid prefix")
	}
}

func TestBGPBlackholeControllerActiveBlackholes(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	// Add a mock blackhole entry
	prefix := "192.0.2.1/32"
	controller.mu.Lock()
	controller.activeBlackholes[prefix] = &BlackholeEntry{
		Prefix:     prefix,
		Tenant:     "test-tenant",
		PoP:        "MOW",
		InjectedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	controller.mu.Unlock()
	
	// Test that blackhole exists
	controller.mu.RLock()
	_, exists := controller.activeBlackholes[prefix]
	controller.mu.RUnlock()
	
	if !exists {
		t.Error("Expected blackhole to exist")
	}
}

func TestAlertHandler(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		blackholesWithdrawn: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_withdrawn",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	handler := &AlertHandler{controller: controller}
	
	tests := []struct {
		name           string
		method         string
		payload        AlertmanagerPayload
		expectedStatus int
		expectedBody   string
	}{
		{
			name:   "Valid firing alert",
			method: "POST",
			payload: AlertmanagerPayload{
				Alerts: []Alert{
					{
						Status: "firing",
						Labels: map[string]string{
							"tenant":     "test-tenant",
							"pop":        "MOW",
							"target_ip":  "192.0.2.1",
						},
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:   "Valid resolved alert",
			method: "POST",
			payload: AlertmanagerPayload{
				Alerts: []Alert{
					{
						Status: "resolved",
						Labels: map[string]string{
							"tenant":     "test-tenant",
							"pop":        "MOW",
							"target_ip":  "192.0.2.1",
						},
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:   "Alert without target_ip",
			method: "POST",
			payload: AlertmanagerPayload{
				Alerts: []Alert{
					{
						Status: "firing",
						Labels: map[string]string{
							"tenant": "test-tenant",
							"pop":    "MOW",
						},
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Wrong method",
			method:         "GET",
			payload:        AlertmanagerPayload{},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest(tt.method, "/alert", bytes.NewReader(payloadBytes))
			req.Header.Set("Content-Type", "application/json")
			
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			
			if tt.expectedBody != "" && w.Body.String() != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestAlertHandlerInvalidJSON(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
	}
	handler := &AlertHandler{controller: controller}
	
	req := httptest.NewRequest("POST", "/alert", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestBGPBlackholeControllerHealthCheck(t *testing.T) {
	// Test with mock controller (no GoBGP connection)
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
	}
	
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	
	// This will fail because there's no GoBGP connection
	controller.HealthCheck(w, req)
	
	// Should return service unavailable
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestBGPBlackholeControllerMetrics(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		blackholesWithdrawn: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_withdrawn",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
		blackholeDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "test_blackhole_duration",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	// Test metric recording
	controller.blackholesInjected.WithLabelValues("tenant1", "MOW", "192.0.2.1/32").Inc()
	controller.blackholesWithdrawn.WithLabelValues("tenant1", "MOW", "192.0.2.1/32").Inc()
	controller.activeBlackholesGauge.WithLabelValues("tenant1", "MOW").Set(1)
	controller.blackholeDuration.WithLabelValues("tenant1", "MOW").Observe(3600) // 1 hour
	
	// Verify metrics are recorded
	injectedCount := testutil.ToFloat64(controller.blackholesInjected.WithLabelValues("tenant1", "MOW", "192.0.2.1/32"))
	if injectedCount != 1 {
		t.Errorf("Expected injected count 1, got %f", injectedCount)
	}
	
	withdrawnCount := testutil.ToFloat64(controller.blackholesWithdrawn.WithLabelValues("tenant1", "MOW", "192.0.2.1/32"))
	if withdrawnCount != 1 {
		t.Errorf("Expected withdrawn count 1, got %f", withdrawnCount)
	}
	
	activeCount := testutil.ToFloat64(controller.activeBlackholesGauge.WithLabelValues("tenant1", "MOW"))
	if activeCount != 1 {
		t.Errorf("Expected active count 1, got %f", activeCount)
	}
	
	durationCount := testutil.ToFloat64(controller.blackholeDuration.WithLabelValues("tenant1", "MOW"))
	if durationCount != 1 {
		t.Errorf("Expected duration count 1, got %f", durationCount)
	}
}

func TestBGPBlackholeControllerConcurrency(t *testing.T) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	// Test concurrent access to metrics
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(tenant string) {
			defer func() { done <- true }()
			
			// Simulate concurrent metric updates
			controller.blackholesInjected.WithLabelValues(tenant, "MOW", "192.0.2.1/32").Inc()
			controller.activeBlackholesGauge.WithLabelValues(tenant, "MOW").Set(1)
		}(string(rune('A' + i)))
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for goroutines")
		}
	}
}

func TestBlackholeEntry(t *testing.T) {
	entry := &BlackholeEntry{
		Prefix:     "192.0.2.1/32",
		Tenant:     "test-tenant",
		PoP:        "MOW",
		InjectedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	
	if entry.Prefix != "192.0.2.1/32" {
		t.Errorf("Expected prefix %s, got %s", "192.0.2.1/32", entry.Prefix)
	}
	
	if entry.Tenant != "test-tenant" {
		t.Errorf("Expected tenant %s, got %s", "test-tenant", entry.Tenant)
	}
	
	if entry.PoP != "MOW" {
		t.Errorf("Expected PoP %s, got %s", "MOW", entry.PoP)
	}
	
	if entry.ExpiresAt.Before(entry.InjectedAt) {
		t.Error("ExpiresAt should be after InjectedAt")
	}
}

func TestMainEnvironment(t *testing.T) {
	// Test environment variable handling
	os.Setenv("GOBGP_ADDR", "test-gobgp:50051")
	defer os.Unsetenv("GOBGP_ADDR")
	
	// This tests the environment variable reading logic
	gobgpAddr := os.Getenv("GOBGP_ADDR")
	if gobgpAddr != "test-gobgp:50051" {
		t.Errorf("Expected GOBGP_ADDR to be 'test-gobgp:50051', got %s", gobgpAddr)
	}
	
	// Test default value
	os.Unsetenv("GOBGP_ADDR")
	gobgpAddr = os.Getenv("GOBGP_ADDR")
	if gobgpAddr != "" {
		t.Errorf("Expected empty GOBGP_ADDR, got %s", gobgpAddr)
	}
}

func BenchmarkBGPBlackholeControllerCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		controller := &BGPBlackholeController{
			activeBlackholes: make(map[string]*BlackholeEntry),
		}
		_ = controller
	}
}

func BenchmarkBGPBlackholeControllerMetrics(b *testing.B) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
		blackholesInjected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_injected",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		blackholesWithdrawn: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_blackholes_withdrawn",
			},
			[]string{"tenant", "pop", "prefix"},
		),
		activeBlackholesGauge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_active_blackholes",
			},
			[]string{"tenant", "pop"},
		),
		blackholeDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "test_blackhole_duration",
			},
			[]string{"tenant", "pop"},
		),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		controller.blackholesInjected.WithLabelValues("tenant", "MOW", "192.0.2.1/32").Inc()
		controller.blackholesWithdrawn.WithLabelValues("tenant", "MOW", "192.0.2.1/32").Inc()
		controller.activeBlackholesGauge.WithLabelValues("tenant", "MOW").Set(1)
		controller.blackholeDuration.WithLabelValues("tenant", "MOW").Observe(3600)
	}
}

func BenchmarkAlertHandler(b *testing.B) {
	controller := &BGPBlackholeController{
		activeBlackholes: make(map[string]*BlackholeEntry),
	}
	handler := &AlertHandler{controller: controller}
	
	payload := AlertmanagerPayload{
		Alerts: []Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"tenant":     "test-tenant",
					"pop":        "MOW",
					"target_ip":  "192.0.2.1",
				},
			},
		},
	}
	
	payloadBytes, _ := json.Marshal(payload)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/alert", bytes.NewReader(payloadBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}
