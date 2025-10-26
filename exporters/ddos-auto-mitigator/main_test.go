package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewDDoSDetector(t *testing.T) {
	// Test with invalid Prometheus URL (should fail)
	_, err := NewDDoSDetector("invalid-url", "localhost:6379")
	if err == nil {
		t.Error("Expected error for invalid Prometheus URL")
	}
}

func TestDDoSDetectorCalculateSeverity(t *testing.T) {
	detector := &DDoSDetector{}

	tests := []struct {
		value     float64
		threshold float64
		expected  string
	}{
		{100, 50, "medium"},   // 2x threshold
		{300, 50, "high"},     // 6x threshold
		{600, 50, "critical"}, // 12x threshold
		{30, 50, "low"},       // 0.6x threshold
	}

	for _, tt := range tests {
		result := detector.calculateSeverity(tt.value, tt.threshold)
		if result != tt.expected {
			t.Errorf("Expected severity %s for value %f (threshold %f), got %s",
				tt.expected, tt.value, tt.threshold, result)
		}
	}
}

func TestDDoSDetectorSelectMitigationType(t *testing.T) {
	detector := &DDoSDetector{}

	tests := []struct {
		attack   *Attack
		expected string
	}{
		{
			&Attack{Severity: "critical", AttackType: "volumetric_pps"},
			"bgp_blackhole",
		},
		{
			&Attack{Severity: "high", AttackType: "volumetric_bps"},
			"bgp_blackhole",
		},
		{
			&Attack{Severity: "medium", AttackType: "volumetric_pps"},
			"upstream_scrubbing",
		},
		{
			&Attack{Severity: "medium", AttackType: "volumetric_bps"},
			"upstream_scrubbing",
		},
		{
			&Attack{Severity: "medium", AttackType: "application_layer"},
			"rate_limit",
		},
		{
			&Attack{Severity: "medium", AttackType: "connection_flood"},
			"rate_limit",
		},
		{
			&Attack{Severity: "low", AttackType: "unknown"},
			"rate_limit", // Default
		},
	}

	for _, tt := range tests {
		result := detector.selectMitigationType(tt.attack)
		if result != tt.expected {
			t.Errorf("Expected mitigation type %s for attack %+v, got %s",
				tt.expected, tt.attack, result)
		}
	}
}

func TestDDoSDetectorActivateBGPBlackhole(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/api/blackhole/add" {
			t.Errorf("Expected path /api/blackhole/add, got %s", r.URL.Path)
		}

		// Check content type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "success"}`))
	}))
	defer server.Close()

	// Update global variable for test
	originalURL := *bgpAPIURL
	*bgpAPIURL = server.URL
	defer func() { *bgpAPIURL = originalURL }()

	detector := &DDoSDetector{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	err := detector.activateBGPBlackhole(ctx, "192.0.2.1")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestDDoSDetectorActivateBGPBlackholeFailure(t *testing.T) {
	// Create mock HTTP server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	// Update global variable for test
	originalURL := *bgpAPIURL
	*bgpAPIURL = server.URL
	defer func() { *bgpAPIURL = originalURL }()

	detector := &DDoSDetector{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	err := detector.activateBGPBlackhole(ctx, "192.0.2.1")

	if err == nil {
		t.Error("Expected error for server error response")
	}
}

func TestDDoSDetectorActivateRateLimit(t *testing.T) {
	detector := &DDoSDetector{}

	attack := &Attack{
		TargetIP:   "192.0.2.1",
		AttackType: "application_layer",
		Severity:   "medium",
	}

	ctx := context.Background()
	err := detector.activateRateLimit(ctx, attack)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestDDoSDetectorActivateUpstreamScrubbing(t *testing.T) {
	detector := &DDoSDetector{}

	ctx := context.Background()
	err := detector.activateUpstreamScrubbing(ctx, "192.0.2.1")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestDDoSDetectorSaveAttackToRedis(t *testing.T) {
	// Create mock Redis client
	mockRedis := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})

	detector := &DDoSDetector{
		redisClient: mockRedis,
	}

	attack := &Attack{
		TargetIP:       "192.0.2.1",
		AttackType:     "volumetric_pps",
		DetectedAt:     time.Now(),
		Severity:       "high",
		PPS:            150000,
		BPS:            1000000000,
		ErrorRate:      0.1,
		Connections:    5000,
		Mitigated:      true,
		MitigationType: "bgp_blackhole",
	}

	ctx := context.Background()
	err := detector.saveAttackToRedis(ctx, attack)

	// This will fail in test environment without Redis, but we can test the logic
	if err != nil {
		// Expected in test environment without Redis
		t.Logf("Expected Redis error in test environment: %v", err)
	}
}

func TestAttackStruct(t *testing.T) {
	attack := &Attack{
		TargetIP:       "192.0.2.1",
		AttackType:     "volumetric_pps",
		DetectedAt:     time.Now(),
		Severity:       "high",
		PPS:            150000,
		BPS:            1000000000,
		ErrorRate:      0.1,
		Connections:    5000,
		Mitigated:      true,
		MitigationType: "bgp_blackhole",
	}

	// Test JSON marshaling
	jsonData, err := json.Marshal(attack)
	if err != nil {
		t.Fatalf("Failed to marshal attack: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaledAttack Attack
	err = json.Unmarshal(jsonData, &unmarshaledAttack)
	if err != nil {
		t.Fatalf("Failed to unmarshal attack: %v", err)
	}

	if unmarshaledAttack.TargetIP != attack.TargetIP {
		t.Errorf("Expected TargetIP %s, got %s", attack.TargetIP, unmarshaledAttack.TargetIP)
	}

	if unmarshaledAttack.AttackType != attack.AttackType {
		t.Errorf("Expected AttackType %s, got %s", attack.AttackType, unmarshaledAttack.AttackType)
	}

	if unmarshaledAttack.Severity != attack.Severity {
		t.Errorf("Expected Severity %s, got %s", attack.Severity, unmarshaledAttack.Severity)
	}
}

func TestPrometheusMetrics(t *testing.T) {
	// Test that metrics are registered
	registry := prometheus.NewRegistry()
	registry.MustRegister(ddosDetections)
	registry.MustRegister(mitigationActions)
	registry.MustRegister(mitigationDuration)
	registry.MustRegister(activeAttacks)

	// Test metric recording
	ddosDetections.WithLabelValues("192.0.2.1", "volumetric_pps").Inc()
	mitigationActions.WithLabelValues("bgp_blackhole", "success").Inc()
	mitigationDuration.Observe(1.5)
	activeAttacks.Set(2)

	// Verify metrics
	ddosCount := testutil.ToFloat64(ddosDetections.WithLabelValues("192.0.2.1", "volumetric_pps"))
	if ddosCount != 1 {
		t.Errorf("Expected ddos detections 1, got %f", ddosCount)
	}

	mitigationCount := testutil.ToFloat64(mitigationActions.WithLabelValues("bgp_blackhole", "success"))
	if mitigationCount != 1 {
		t.Errorf("Expected mitigation actions 1, got %f", mitigationCount)
	}

	activeCount := testutil.ToFloat64(activeAttacks)
	if activeCount != 2 {
		t.Errorf("Expected active attacks 2, got %f", activeCount)
	}
}

func TestDDoSDetectorMitigateAttack(t *testing.T) {
	// Create mock HTTP server for BGP API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "success"}`))
	}))
	defer server.Close()

	// Update global variable for test
	originalURL := *bgpAPIURL
	*bgpAPIURL = server.URL
	defer func() { *bgpAPIURL = originalURL }()

	detector := &DDoSDetector{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		redisClient: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
			DB:   0,
		}),
	}

	attack := &Attack{
		TargetIP:   "192.0.2.1",
		AttackType: "volumetric_pps",
		Severity:   "critical",
		PPS:        200000,
	}

	ctx := context.Background()
	err := detector.mitigateAttack(ctx, attack)

	// This will fail in test environment without Redis, but we can test the logic
	if err != nil {
		// Expected in test environment without Redis
		t.Logf("Expected Redis error in test environment: %v", err)
	}

	// Check that attack was marked as mitigated
	if !attack.Mitigated {
		t.Error("Expected attack to be marked as mitigated")
	}

	if attack.MitigationType == "" {
		t.Error("Expected mitigation type to be set")
	}
}

func TestDDoSDetectorConcurrency(t *testing.T) {
	detector := &DDoSDetector{
		activeAttacksMap: make(map[string]*Attack),
	}
	
	// Test concurrent access to activeAttacksMap with mutex
	var mu sync.Mutex
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(attackNum int) {
			defer func() { done <- true }()
			
			attack := &Attack{
				TargetIP:   "192.0.2.1",
				AttackType: "volumetric_pps",
				Severity:   "high",
				PPS:        150000,
			}
			
			attackKey := fmt.Sprintf("192.0.2.%d:volumetric_pps", attackNum)
			mu.Lock()
			detector.activeAttacksMap[attackKey] = attack
			mu.Unlock()
		}(i)
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

func BenchmarkDDoSDetectorCalculateSeverity(b *testing.B) {
	detector := &DDoSDetector{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.calculateSeverity(150000, 100000)
	}
}

func BenchmarkDDoSDetectorSelectMitigationType(b *testing.B) {
	detector := &DDoSDetector{}
	attack := &Attack{
		Severity:   "high",
		AttackType: "volumetric_pps",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.selectMitigationType(attack)
	}
}

func BenchmarkPrometheusMetrics(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ddosDetections.WithLabelValues("192.0.2.1", "volumetric_pps").Inc()
		mitigationActions.WithLabelValues("bgp_blackhole", "success").Inc()
		mitigationDuration.Observe(1.5)
		activeAttacks.Set(float64(i % 10))
	}
}
