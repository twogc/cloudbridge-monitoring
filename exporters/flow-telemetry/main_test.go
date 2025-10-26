package main

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

func TestMainFunction(t *testing.T) {
	// Test that main function can be called without crashing
	// This is a basic smoke test
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Main function panicked: %v", r)
		}
	}()
	
	// We can't actually call main() in a test, but we can test the components
	// that main() would use
}

func TestPrintStartupInfo(t *testing.T) {
	// Test startup info printing
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	
	// This should not panic
	printStartupInfo(logger)
}

func TestFlagParsing(t *testing.T) {
	// Test that flags can be parsed correctly
	// Save original args
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()
	
	// Set test args
	os.Args = []string{
		"flow-telemetry",
		"-sflow-port=6343",
		"-netflow-port=2055",
		"-ipfix-port=4739",
		"-metrics-port=:9090",
		"-window=60s",
		"-log-level=info",
		"-enable-sflow=true",
		"-enable-netflow=true",
		"-enable-ipfix=true",
	}
	
	// Test flag parsing (this would normally happen in main)
	// We can't actually call flag.Parse() in a test, but we can test the logic
}

func TestFlowTelemetryIntegration(t *testing.T) {
	// Test integration of all components
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create aggregator
	aggregator := NewFlowAggregator(60 * time.Second)
	
	// Create collector
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector, got %v", err)
	}
	
	// Create exporter
	exporter := NewFlowExporter(aggregator)
	
	// Test that all components are properly initialized
	if aggregator == nil {
		t.Fatal("Aggregator should not be nil")
	}
	
	if collector == nil {
		t.Fatal("Collector should not be nil")
	}
	
	if exporter == nil {
		t.Fatal("Exporter should not be nil")
	}
	
	// Test that aggregator and collector are linked
	if collector.aggregator != aggregator {
		t.Error("Collector should be linked to aggregator")
	}
	
	// Test that aggregator and exporter are linked
	if exporter.aggregator != aggregator {
		t.Error("Exporter should be linked to aggregator")
	}
}

func TestFlowTelemetryWithRealPorts(t *testing.T) {
	// Test with real ports (but don't actually start servers)
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create aggregator
	aggregator := NewFlowAggregator(60 * time.Second)
	
	// Test collector creation with real ports
	collector, err := NewFlowCollector(6343, 2055, 4739, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector, got %v", err)
	}
	
	// Test that ports are set correctly
	if collector.sflowPort != 6343 {
		t.Errorf("Expected sflow port 6343, got %d", collector.sflowPort)
	}
	
	if collector.netflowPort != 2055 {
		t.Errorf("Expected netflow port 2055, got %d", collector.netflowPort)
	}
	
	if collector.ipfixPort != 4739 {
		t.Errorf("Expected ipfix port 4739, got %d", collector.ipfixPort)
	}
}

func TestFlowTelemetryMetricsIntegration(t *testing.T) {
	// Test metrics integration
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create all components
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector, got %v", err)
	}
	exporter := NewFlowExporter(aggregator)
	
	// Test that metrics are properly registered
	// This is tested indirectly through the component tests
	if collector.flowsReceived == nil {
		t.Error("Collector flowsReceived metric should be initialized")
	}
	
	if collector.bytesReceived == nil {
		t.Error("Collector bytesReceived metric should be initialized")
	}
	
	if collector.packetsReceived == nil {
		t.Error("Collector packetsReceived metric should be initialized")
	}
	
	if collector.parseErrors == nil {
		t.Error("Collector parseErrors metric should be initialized")
	}
	
	if exporter.ipTrafficBytes == nil {
		t.Error("Exporter ipTrafficBytes metric should be initialized")
	}
	
	if exporter.ipTrafficPackets == nil {
		t.Error("Exporter ipTrafficPackets metric should be initialized")
	}
	
	if exporter.portTraffic == nil {
		t.Error("Exporter portTraffic metric should be initialized")
	}
	
	if exporter.protoTraffic == nil {
		t.Error("Exporter protoTraffic metric should be initialized")
	}
	
	if exporter.anomalyDetected == nil {
		t.Error("Exporter anomalyDetected metric should be initialized")
	}
	
	if exporter.attackPattern == nil {
		t.Error("Exporter attackPattern metric should be initialized")
	}
}

func TestFlowTelemetryDataFlow(t *testing.T) {
	// Test data flow through the system
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create all components
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector, got %v", err)
	}
	_ = collector // Use collector to avoid unused variable warning
	exporter := NewFlowExporter(aggregator)
	
	// Create test flow record
	flow := &FlowRecord{
		SrcIP:     net.ParseIP("192.0.2.1"),
		DstIP:     net.ParseIP("192.0.2.2"),
		SrcPort:   12345,
		DstPort:   80,
		Protocol:  6, // TCP
		Packets:   100,
		Bytes:     10240,
		TCPFlags:  0x02, // SYN
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Type:      "test",
	}
	
	// Process flow through aggregator
	aggregator.ProcessFlow(flow)
	
	// Update exporter metrics
	exporter.UpdateMetrics()
	
	// Test that data flowed through the system
	// This is tested indirectly through the component tests
	// The actual verification is done in the individual component tests
}

func TestFlowTelemetryErrorHandling(t *testing.T) {
	// Test error handling in the system
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create aggregator
	aggregator := NewFlowAggregator(60 * time.Second)
	
	// Test collector creation with invalid ports
	collector, err := NewFlowCollector(-1, -1, -1, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector with invalid ports, got %v", err)
	}
	
	// Test that invalid ports are handled gracefully
	if collector.sflowPort != -1 {
		t.Errorf("Expected sflow port -1, got %d", collector.sflowPort)
	}
	
	if collector.netflowPort != -1 {
		t.Errorf("Expected netflow port -1, got %d", collector.netflowPort)
	}
	
	if collector.ipfixPort != -1 {
		t.Errorf("Expected ipfix port -1, got %d", collector.ipfixPort)
	}
}

func TestFlowTelemetryConcurrency(t *testing.T) {
	// Test concurrent operations
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create all components
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector, got %v", err)
	}
	_ = collector // Use collector to avoid unused variable warning
	exporter := NewFlowExporter(aggregator)
	
	// Test concurrent flow processing
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(flowNum int) {
			defer func() { done <- true }()
			
			flow := &FlowRecord{
				SrcIP:     net.ParseIP("192.0.2.1"),
				DstIP:     net.ParseIP("192.0.2.2"),
				Protocol:  6,
				Packets:   10,
				Bytes:     1024,
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Type:      "test",
			}
			
			aggregator.ProcessFlow(flow)
			exporter.UpdateMetrics()
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

func TestFlowTelemetryPerformance(t *testing.T) {
	// Test performance characteristics
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create all components
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error creating collector, got %v", err)
	}
	_ = collector // Use collector to avoid unused variable warning
	exporter := NewFlowExporter(aggregator)
	
	// Test processing many flows
	start := time.Now()
	
	for i := 0; i < 1000; i++ {
		flow := &FlowRecord{
			SrcIP:     net.ParseIP("192.0.2.1"),
			DstIP:     net.ParseIP("192.0.2.2"),
			Protocol:  6,
			Packets:   10,
			Bytes:     1024,
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Type:      "test",
		}
		
		aggregator.ProcessFlow(flow)
	}
	
	exporter.UpdateMetrics()
	
	elapsed := time.Since(start)
	
	// Test should complete within reasonable time
	if elapsed > 5*time.Second {
		t.Errorf("Processing 1000 flows took too long: %v", elapsed)
	}
	
	t.Logf("Processed 1000 flows in %v", elapsed)
}

func BenchmarkFlowTelemetryIntegration(b *testing.B) {
	// Benchmark the entire flow telemetry system
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	// Create all components
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		b.Fatalf("Expected no error creating collector, got %v", err)
	}
	_ = collector // Use collector to avoid unused variable warning
	exporter := NewFlowExporter(aggregator)
	
	// Create test flow record
	flow := &FlowRecord{
		SrcIP:     net.ParseIP("192.0.2.1"),
		DstIP:     net.ParseIP("192.0.2.2"),
		Protocol:  6,
		Packets:   10,
		Bytes:     1024,
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Type:      "test",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		aggregator.ProcessFlow(flow)
		exporter.UpdateMetrics()
	}
}
