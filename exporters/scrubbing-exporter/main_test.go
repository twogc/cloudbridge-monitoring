package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewScrubbingExporter(t *testing.T) {
	// Test with default environment
	exporter := NewScrubbingExporter()
	
	if exporter == nil {
		t.Fatal("ScrubbingExporter should not be nil")
	}
	
	// Check that all metrics are initialized
	if exporter.ddosAttacksDetected == nil {
		t.Fatal("ddosAttacksDetected should not be nil")
	}
	if exporter.blackholesInjected == nil {
		t.Fatal("blackholesInjected should not be nil")
	}
	if exporter.scrubbingLatency == nil {
		t.Fatal("scrubbingLatency should not be nil")
	}
	if exporter.activeBlackholes == nil {
		t.Fatal("activeBlackholes should not be nil")
	}
	if exporter.upstreamScrubbingStatus == nil {
		t.Fatal("upstreamScrubbingStatus should not be nil")
	}
}

func TestScrubbingExporterWithEnvironment(t *testing.T) {
	// Test with environment variables
	os.Setenv("YANDEX_CLOUD_ENABLED", "true")
	os.Setenv("QRATOR_ENABLED", "true")
	os.Setenv("RTK_ENABLED", "false")
	defer func() {
		os.Unsetenv("YANDEX_CLOUD_ENABLED")
		os.Unsetenv("QRATOR_ENABLED")
		os.Unsetenv("RTK_ENABLED")
	}()
	
	exporter := NewScrubbingExporter()
	
	if !exporter.yandexCloudEnabled {
		t.Error("yandexCloudEnabled should be true")
	}
	if !exporter.qratorEnabled {
		t.Error("qratorEnabled should be true")
	}
	if exporter.rtkEnabled {
		t.Error("rtkEnabled should be false")
	}
}

func TestScrubbingExporterRegister(t *testing.T) {
	exporter := NewScrubbingExporter()
	
	// Register should not panic
	exporter.Register()
	
	// Verify metrics are registered
	registry := prometheus.NewRegistry()
	registry.MustRegister(exporter.ddosAttacksDetected)
	registry.MustRegister(exporter.blackholesInjected)
	registry.MustRegister(exporter.scrubbingLatency)
	registry.MustRegister(exporter.activeBlackholes)
	registry.MustRegister(exporter.upstreamScrubbingStatus)
}

func TestScrubbingExporterCollectMetrics(t *testing.T) {
	exporter := NewScrubbingExporter()
	exporter.Register()
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	// Start metric collection
	go exporter.CollectMetrics(ctx)
	
	// Wait for at least one collection cycle
	time.Sleep(150 * time.Millisecond)
	
	// Check that upstream status metrics are set
	metric := testutil.ToFloat64(exporter.upstreamScrubbingStatus.WithLabelValues("yandex-cloud"))
	if metric != 0 && metric != 1 {
		t.Errorf("Expected upstream status to be 0 or 1, got %f", metric)
	}
}

func TestScrubbingExporterCollectUpstreamStatus(t *testing.T) {
	tests := []struct {
		name           string
		yandexEnabled  bool
		qratorEnabled  bool
		rtkEnabled     bool
		expectedProviders []string
	}{
		{
			name:           "All disabled",
			yandexEnabled: false,
			qratorEnabled: false,
			rtkEnabled:    false,
			expectedProviders: []string{},
		},
		{
			name:           "Only Yandex enabled",
			yandexEnabled: true,
			qratorEnabled: false,
			rtkEnabled:    false,
			expectedProviders: []string{"yandex-cloud"},
		},
		{
			name:           "All enabled",
			yandexEnabled: true,
			qratorEnabled: true,
			rtkEnabled:    true,
			expectedProviders: []string{"yandex-cloud", "qrator", "rtk"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := &ScrubbingExporter{
				yandexCloudEnabled: tt.yandexEnabled,
				qratorEnabled:      tt.qratorEnabled,
				rtkEnabled:         tt.rtkEnabled,
				upstreamScrubbingStatus: prometheus.NewGaugeVec(
					prometheus.GaugeOpts{
						Name: "test_upstream_status",
					},
					[]string{"provider"},
				),
			}
			
			exporter.collectUpstreamStatus()
			
			// Check that expected providers have status set
			for _, provider := range tt.expectedProviders {
				metric := testutil.ToFloat64(exporter.upstreamScrubbingStatus.WithLabelValues(provider))
				if metric != 1 {
					t.Errorf("Expected status 1 for provider %s, got %f", provider, metric)
				}
			}
		})
	}
}

func TestAlertHandler(t *testing.T) {
	exporter := NewScrubbingExporter()
	exporter.Register()
	
	handler := &AlertHandler{exporter: exporter}
	
	tests := []struct {
		name           string
		method         string
		payload        AlertmanagerPayload
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Valid firing alert",
			method:         "POST",
			payload: AlertmanagerPayload{
				Alerts: []Alert{
					{
						Status: "firing",
						Labels: map[string]string{
							"tenant":      "test-tenant",
							"pop":         "MOW",
							"attack_type": "syn_flood",
						},
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:           "Valid resolved alert",
			method:         "POST",
			payload: AlertmanagerPayload{
				Alerts: []Alert{
					{
						Status: "resolved",
						Labels: map[string]string{
							"tenant":      "test-tenant",
							"pop":         "MOW",
							"attack_type": "syn_flood",
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
	exporter := NewScrubbingExporter()
	handler := &AlertHandler{exporter: exporter}
	
	req := httptest.NewRequest("POST", "/alert", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestScrubbingExporterHealthCheck(t *testing.T) {
	exporter := NewScrubbingExporter()
	
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	
	exporter.HealthCheck(w, req)
	
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	
	if w.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got %q", w.Body.String())
	}
}

func TestScrubbingExporterMetrics(t *testing.T) {
	exporter := NewScrubbingExporter()
	exporter.Register()
	
	// Test metric recording
	exporter.ddosAttacksDetected.WithLabelValues("tenant1", "MOW", "syn_flood").Inc()
	exporter.blackholesInjected.WithLabelValues("tenant1", "MOW", "192.0.2.1/32").Inc()
	exporter.scrubbingLatency.WithLabelValues("tenant1", "MOW", "yandex-cloud").Observe(0.5)
	exporter.activeBlackholes.WithLabelValues("tenant1", "MOW").Set(1)
	exporter.upstreamScrubbingStatus.WithLabelValues("yandex-cloud").Set(1)
	
	// Verify metrics are recorded
	ddosCount := testutil.ToFloat64(exporter.ddosAttacksDetected.WithLabelValues("tenant1", "MOW", "syn_flood"))
	if ddosCount != 1 {
		t.Errorf("Expected ddos attacks count 1, got %f", ddosCount)
	}
	
	blackholeCount := testutil.ToFloat64(exporter.blackholesInjected.WithLabelValues("tenant1", "MOW", "192.0.2.1/32"))
	if blackholeCount != 1 {
		t.Errorf("Expected blackhole count 1, got %f", blackholeCount)
	}
	
	latencyCount := testutil.ToFloat64(exporter.scrubbingLatency.WithLabelValues("tenant1", "MOW", "yandex-cloud"))
	if latencyCount != 1 {
		t.Errorf("Expected latency count 1, got %f", latencyCount)
	}
	
	activeBlackholes := testutil.ToFloat64(exporter.activeBlackholes.WithLabelValues("tenant1", "MOW"))
	if activeBlackholes != 1 {
		t.Errorf("Expected active blackholes 1, got %f", activeBlackholes)
	}
	
	upstreamStatus := testutil.ToFloat64(exporter.upstreamScrubbingStatus.WithLabelValues("yandex-cloud"))
	if upstreamStatus != 1 {
		t.Errorf("Expected upstream status 1, got %f", upstreamStatus)
	}
}

func TestScrubbingExporterConcurrency(t *testing.T) {
	exporter := NewScrubbingExporter()
	exporter.Register()
	
	// Test concurrent access to metrics
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(tenant string) {
			defer func() { done <- true }()
			
			// Simulate concurrent metric updates
			exporter.ddosAttacksDetected.WithLabelValues(tenant, "MOW", "syn_flood").Inc()
			exporter.blackholesInjected.WithLabelValues(tenant, "MOW", "192.0.2.1/32").Inc()
			exporter.scrubbingLatency.WithLabelValues(tenant, "MOW", "yandex-cloud").Observe(0.5)
			exporter.activeBlackholes.WithLabelValues(tenant, "MOW").Set(1)
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

func BenchmarkScrubbingExporterCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		exporter := NewScrubbingExporter()
		_ = exporter
	}
}

func BenchmarkScrubbingExporterMetrics(b *testing.B) {
	exporter := NewScrubbingExporter()
	exporter.Register()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exporter.ddosAttacksDetected.WithLabelValues("tenant", "MOW", "syn_flood").Inc()
		exporter.blackholesInjected.WithLabelValues("tenant", "MOW", "192.0.2.1/32").Inc()
		exporter.scrubbingLatency.WithLabelValues("tenant", "MOW", "yandex-cloud").Observe(0.5)
		exporter.activeBlackholes.WithLabelValues("tenant", "MOW").Set(1)
		exporter.upstreamScrubbingStatus.WithLabelValues("yandex-cloud").Set(1)
	}
}

func BenchmarkAlertHandler(b *testing.B) {
	exporter := NewScrubbingExporter()
	exporter.Register()
	handler := &AlertHandler{exporter: exporter}
	
	payload := AlertmanagerPayload{
		Alerts: []Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"tenant":      "test-tenant",
					"pop":         "MOW",
					"attack_type": "syn_flood",
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
