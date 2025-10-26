package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewFlowCollector(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	
	// Test with all ports enabled
	collector, err := NewFlowCollector(6343, 2055, 4739, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	if collector == nil {
		t.Fatal("Collector should not be nil")
	}
	
	if collector.sflowPort != 6343 {
		t.Errorf("Expected sflow port 6343, got %d", collector.sflowPort)
	}
	
	if collector.netflowPort != 2055 {
		t.Errorf("Expected netflow port 2055, got %d", collector.netflowPort)
	}
	
	if collector.ipfixPort != 4739 {
		t.Errorf("Expected ipfix port 4739, got %d", collector.ipfixPort)
	}
	
	if collector.aggregator != aggregator {
		t.Error("Expected aggregator to be set")
	}
}

func TestNewFlowCollectorDisabledPorts(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	
	// Test with all ports disabled
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	if collector.sflowPort != 0 {
		t.Errorf("Expected sflow port 0, got %d", collector.sflowPort)
	}
	
	if collector.netflowPort != 0 {
		t.Errorf("Expected netflow port 0, got %d", collector.netflowPort)
	}
	
	if collector.ipfixPort != 0 {
		t.Errorf("Expected ipfix port 0, got %d", collector.ipfixPort)
	}
}

func TestFlowCollectorStartStop(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Expected no error starting collector, got %v", err)
	}
	
	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Fatalf("Expected no error stopping collector, got %v", err)
	}
}

func TestFlowCollectorStartAlreadyRunning(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	ctx := context.Background()
	
	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Expected no error starting collector, got %v", err)
	}
	
	// Try to start again
	err = collector.Start(ctx)
	if err == nil {
		t.Error("Expected error for already running collector")
	}
	
	// Stop collector
	collector.Stop()
}

func TestFlowCollectorStopNotRunning(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	// Stop collector that's not running
	err = collector.Stop()
	if err != nil {
		t.Errorf("Expected no error stopping non-running collector, got %v", err)
	}
}

func TestFlowCollectorParseSFlowDatagram(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	// Test with empty data
	flows, err := collector.parseSFlowDatagram([]byte{}, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for empty data")
	}
	
	// Test with too short data
	shortData := make([]byte, 10)
	flows, err = collector.parseSFlowDatagram(shortData, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for too short data")
	}
	
	// Test with invalid version
	invalidData := make([]byte, 28)
	invalidData[0] = 0x00 // Version 0
	invalidData[1] = 0x00
	invalidData[2] = 0x00
	invalidData[3] = 0x00
	flows, err = collector.parseSFlowDatagram(invalidData, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for invalid version")
	}
	
	// Test with valid sFlow v5 header but no samples
	validData := make([]byte, 28)
	validData[0] = 0x00 // Version 5
	validData[1] = 0x00
	validData[2] = 0x00
	validData[3] = 0x05
	validData[24] = 0x00 // Number of samples = 0
	validData[25] = 0x00
	validData[26] = 0x00
	validData[27] = 0x00
	
	flows, err = collector.parseSFlowDatagram(validData, net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Errorf("Expected no error for valid header, got %v", err)
	}
	
	if len(flows) != 0 {
		t.Errorf("Expected 0 flows, got %d", len(flows))
	}
}

func TestFlowCollectorParseNetFlowPacket(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	// Test with empty data
	flows, err := collector.parseNetFlowPacket([]byte{}, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for empty data")
	}
	
	// Test with too short data
	shortData := make([]byte, 10)
	flows, err = collector.parseNetFlowPacket(shortData, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for too short data")
	}
	
	// Test with invalid version
	invalidData := make([]byte, 24)
	invalidData[0] = 0x00 // Version 0
	invalidData[1] = 0x00
	flows, err = collector.parseNetFlowPacket(invalidData, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for invalid version")
	}
	
	// Test with valid NetFlow v5 header but no records
	validData := make([]byte, 24)
	validData[0] = 0x00 // Version 5
	validData[1] = 0x05
	validData[2] = 0x00 // Count = 0
	validData[3] = 0x00
	
	flows, err = collector.parseNetFlowPacket(validData, net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Errorf("Expected no error for valid header, got %v", err)
	}
	
	if len(flows) != 0 {
		t.Errorf("Expected 0 flows, got %d", len(flows))
	}
}

func TestFlowCollectorParseIPFIXMessage(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	// Test with empty data
	flows, err := collector.parseIPFIXMessage([]byte{}, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for empty data")
	}
	
	// Test with too short data
	shortData := make([]byte, 10)
	flows, err = collector.parseIPFIXMessage(shortData, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for too short data")
	}
	
	// Test with invalid version
	invalidData := make([]byte, 16)
	invalidData[0] = 0x00 // Version 0
	invalidData[1] = 0x00
	flows, err = collector.parseIPFIXMessage(invalidData, net.ParseIP("192.0.2.1"))
	if err == nil {
		t.Error("Expected error for invalid version")
	}
	
	// Test with valid IPFIX header
	validData := make([]byte, 16)
	validData[0] = 0x00 // Version 10
	validData[1] = 0x0A
	
	flows, err = collector.parseIPFIXMessage(validData, net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Errorf("Expected no error for valid header, got %v", err)
	}
	
	if len(flows) != 1 {
		t.Errorf("Expected 1 flow, got %d", len(flows))
	}
}

func TestProtocolName(t *testing.T) {
	tests := []struct {
		proto    uint8
		expected string
	}{
		{1, "icmp"},
		{6, "tcp"},
		{17, "udp"},
		{50, "proto_50"},
		{255, "proto_255"},
	}
	
	for _, tt := range tests {
		result := protocolName(tt.proto)
		if result != tt.expected {
			t.Errorf("Expected protocol name %s for proto %d, got %s", tt.expected, tt.proto, result)
		}
	}
}

func TestFlowCollectorMetrics(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	// Test metric recording
	collector.flowsReceived.WithLabelValues("sflow", "192.0.2.1").Inc()
	collector.bytesReceived.WithLabelValues("sflow", "tcp").Add(1024)
	collector.packetsReceived.WithLabelValues("sflow", "tcp").Add(10)
	collector.parseErrors.WithLabelValues("sflow", "parse_error").Inc()
	
	// Verify metrics are recorded
	flowsCount := testutil.ToFloat64(collector.flowsReceived.WithLabelValues("sflow", "192.0.2.1"))
	if flowsCount != 1 {
		t.Errorf("Expected flows count 1, got %f", flowsCount)
	}
	
	bytesCount := testutil.ToFloat64(collector.bytesReceived.WithLabelValues("sflow", "tcp"))
	if bytesCount != 1024 {
		t.Errorf("Expected bytes count 1024, got %f", bytesCount)
	}
	
	packetsCount := testutil.ToFloat64(collector.packetsReceived.WithLabelValues("sflow", "tcp"))
	if packetsCount != 10 {
		t.Errorf("Expected packets count 10, got %f", packetsCount)
	}
	
	errorsCount := testutil.ToFloat64(collector.parseErrors.WithLabelValues("sflow", "parse_error"))
	if errorsCount != 1 {
		t.Errorf("Expected errors count 1, got %f", errorsCount)
	}
}

func TestFlowCollectorConcurrency(t *testing.T) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	// Test concurrent metric updates
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(ipNum int) {
			defer func() { done <- true }()
			
			// Simulate concurrent metric updates
			collector.flowsReceived.WithLabelValues("sflow", "192.0.2.1").Inc()
			collector.bytesReceived.WithLabelValues("sflow", "tcp").Add(1024)
			collector.packetsReceived.WithLabelValues("sflow", "tcp").Add(10)
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

func BenchmarkFlowCollectorParseSFlowDatagram(b *testing.B) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		b.Fatalf("Expected no error, got %v", err)
	}
	
	// Create valid sFlow v5 datagram
	data := make([]byte, 28)
	data[0] = 0x00 // Version 5
	data[1] = 0x00
	data[2] = 0x00
	data[3] = 0x05
	data[24] = 0x00 // Number of samples = 0
	data[25] = 0x00
	data[26] = 0x00
	data[27] = 0x00
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.parseSFlowDatagram(data, net.ParseIP("192.0.2.1"))
	}
}

func BenchmarkFlowCollectorParseNetFlowPacket(b *testing.B) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		b.Fatalf("Expected no error, got %v", err)
	}
	
	// Create valid NetFlow v5 packet
	data := make([]byte, 24)
	data[0] = 0x00 // Version 5
	data[1] = 0x05
	data[2] = 0x00 // Count = 0
	data[3] = 0x00
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.parseNetFlowPacket(data, net.ParseIP("192.0.2.1"))
	}
}

func BenchmarkFlowCollectorParseIPFIXMessage(b *testing.B) {
	// Reset Prometheus registry to avoid duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	
	aggregator := NewFlowAggregator(60 * time.Second)
	collector, err := NewFlowCollector(0, 0, 0, aggregator)
	if err != nil {
		b.Fatalf("Expected no error, got %v", err)
	}
	
	// Create valid IPFIX message
	data := make([]byte, 16)
	data[0] = 0x00 // Version 10
	data[1] = 0x0A
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.parseIPFIXMessage(data, net.ParseIP("192.0.2.1"))
	}
}
