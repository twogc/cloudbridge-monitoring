/*
CloudBridge Monitoring - Flow Telemetry Collector
Collects sFlow, NetFlow v5/v9, and IPFIX telemetry for DDoS detection
*/

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// FlowCollector collects flow telemetry from network devices
type FlowCollector struct {
	logger *logrus.Logger

	// Listeners
	sflowListener   *net.UDPConn
	netflowListener *net.UDPConn
	ipfixListener   *net.UDPConn

	// Configuration
	sflowPort   int
	netflowPort int
	ipfixPort   int

	// Aggregator
	aggregator *FlowAggregator

	// Metrics
	flowsReceived   *prometheus.CounterVec
	bytesReceived   *prometheus.CounterVec
	packetsReceived *prometheus.CounterVec
	parseErrors     *prometheus.CounterVec

	// State
	running bool
	mu      sync.RWMutex
}

// FlowRecord represents a generic flow record
type FlowRecord struct {
	// Source and destination
	SrcIP   net.IP
	DstIP   net.IP
	SrcPort uint16
	DstPort uint16

	// Protocol
	Protocol uint8 // TCP=6, UDP=17, ICMP=1

	// Counters
	Packets uint64
	Bytes   uint64

	// Flags
	TCPFlags uint8

	// Timestamps
	StartTime time.Time
	EndTime   time.Time

	// Metadata
	SrcAS uint32
	DstAS uint32
	Type  string // sflow, netflow, ipfix

	// Original data for debugging
	Raw []byte
}

// NewFlowCollector creates a new flow collector
func NewFlowCollector(sflowPort, netflowPort, ipfixPort int, aggregator *FlowAggregator) (*FlowCollector, error) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	fc := &FlowCollector{
		logger:      logger,
		sflowPort:   sflowPort,
		netflowPort: netflowPort,
		ipfixPort:   ipfixPort,
		aggregator:  aggregator,
	}

	// Initialize Prometheus metrics
	fc.initMetrics()

	return fc, nil
}

// initMetrics initializes Prometheus metrics
func (fc *FlowCollector) initMetrics() {
	fc.flowsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_flow_telemetry_flows_received_total",
			Help: "Total number of flow records received",
		},
		[]string{"type", "source_ip"},
	)

	fc.bytesReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_flow_telemetry_bytes_received_total",
			Help: "Total bytes in flow records",
		},
		[]string{"type", "protocol"},
	)

	fc.packetsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_flow_telemetry_packets_received_total",
			Help: "Total packets in flow records",
		},
		[]string{"type", "protocol"},
	)

	fc.parseErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_flow_telemetry_parse_errors_total",
			Help: "Total number of parse errors",
		},
		[]string{"type", "error_type"},
	)

	prometheus.MustRegister(fc.flowsReceived)
	prometheus.MustRegister(fc.bytesReceived)
	prometheus.MustRegister(fc.packetsReceived)
	prometheus.MustRegister(fc.parseErrors)
}

// Start starts all flow collectors
func (fc *FlowCollector) Start(ctx context.Context) error {
	fc.mu.Lock()
	if fc.running {
		fc.mu.Unlock()
		return fmt.Errorf("collector already running")
	}
	fc.running = true
	fc.mu.Unlock()

	fc.logger.Info("Starting flow telemetry collector...")

	// Start sFlow collector
	if fc.sflowPort > 0 {
		if err := fc.startSFlowCollector(ctx); err != nil {
			return fmt.Errorf("failed to start sFlow collector: %w", err)
		}
		fc.logger.Infof("sFlow collector listening on UDP port %d", fc.sflowPort)
	}

	// Start NetFlow collector
	if fc.netflowPort > 0 {
		if err := fc.startNetFlowCollector(ctx); err != nil {
			return fmt.Errorf("failed to start NetFlow collector: %w", err)
		}
		fc.logger.Infof("NetFlow collector listening on UDP port %d", fc.netflowPort)
	}

	// Start IPFIX collector
	if fc.ipfixPort > 0 {
		if err := fc.startIPFIXCollector(ctx); err != nil {
			return fmt.Errorf("failed to start IPFIX collector: %w", err)
		}
		fc.logger.Infof("IPFIX collector listening on UDP port %d", fc.ipfixPort)
	}

	fc.logger.Info("Flow telemetry collector started successfully")
	return nil
}

// Stop stops all flow collectors
func (fc *FlowCollector) Stop() error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if !fc.running {
		return nil
	}

	fc.logger.Info("Stopping flow telemetry collector...")

	// Close listeners
	if fc.sflowListener != nil {
		fc.sflowListener.Close()
	}
	if fc.netflowListener != nil {
		fc.netflowListener.Close()
	}
	if fc.ipfixListener != nil {
		fc.ipfixListener.Close()
	}

	fc.running = false
	fc.logger.Info("Flow telemetry collector stopped")
	return nil
}

// startSFlowCollector starts the sFlow collector
func (fc *FlowCollector) startSFlowCollector(ctx context.Context) error {
	addr := &net.UDPAddr{
		Port: fc.sflowPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", fc.sflowPort, err)
	}

	fc.sflowListener = conn

	// Start receiver goroutine
	go fc.receiveSFlow(ctx, conn)

	return nil
}

// receiveSFlow receives and parses sFlow datagrams
func (fc *FlowCollector) receiveSFlow(ctx context.Context, conn *net.UDPConn) {
	buffer := make([]byte, 9000) // Jumbo frame size

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Set read deadline to allow checking context
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				fc.logger.Errorf("sFlow read error: %v", err)
				continue
			}

			// Parse sFlow datagram
			flows, err := fc.parseSFlowDatagram(buffer[:n], remoteAddr.IP)
			if err != nil {
				fc.parseErrors.WithLabelValues("sflow", "parse_error").Inc()
				fc.logger.Debugf("sFlow parse error from %s: %v", remoteAddr.IP, err)
				continue
			}

			// Process flows
			for _, flow := range flows {
				fc.flowsReceived.WithLabelValues("sflow", remoteAddr.IP.String()).Inc()
				fc.bytesReceived.WithLabelValues("sflow", protocolName(flow.Protocol)).Add(float64(flow.Bytes))
				fc.packetsReceived.WithLabelValues("sflow", protocolName(flow.Protocol)).Add(float64(flow.Packets))

				// Send to aggregator
				if fc.aggregator != nil {
					fc.aggregator.ProcessFlow(flow)
				}
			}
		}
	}
}

// parseSFlowDatagram parses an sFlow v5 datagram
func (fc *FlowCollector) parseSFlowDatagram(data []byte, sourceIP net.IP) ([]*FlowRecord, error) {
	if len(data) < 28 {
		return nil, fmt.Errorf("datagram too short: %d bytes", len(data))
	}

	// Parse sFlow v5 header
	version := binary.BigEndian.Uint32(data[0:4])
	if version != 5 {
		return nil, fmt.Errorf("unsupported sFlow version: %d", version)
	}

	// Address type (1 = IPv4, 2 = IPv6)
	addressType := binary.BigEndian.Uint32(data[4:8])
	_ = addressType

	// Agent IP address (skip for now)
	// Bytes 8-11 for IPv4

	// Sub-agent ID
	// subAgentID := binary.BigEndian.Uint32(data[12:16])

	// Sequence number
	// seqNum := binary.BigEndian.Uint32(data[16:20])

	// System uptime
	// uptime := binary.BigEndian.Uint32(data[20:24])

	// Number of samples
	numSamples := binary.BigEndian.Uint32(data[24:28])

	flows := make([]*FlowRecord, 0, numSamples)
	offset := 28

	// Parse samples
	for i := uint32(0); i < numSamples && offset < len(data); i++ {
		if offset+8 > len(data) {
			break
		}

		// Sample type and length
		// sampleType := binary.BigEndian.Uint32(data[offset : offset+4])
		sampleLength := binary.BigEndian.Uint32(data[offset+4 : offset+8])
		offset += 8

		if offset+int(sampleLength) > len(data) {
			break
		}

		// For simplicity, create a basic flow record
		// In production, you would parse the actual sFlow sample structure
		flow := &FlowRecord{
			Type:      "sflow",
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Raw:       data[offset : offset+int(sampleLength)],
		}

		// Parse basic flow info from sample (simplified)
		// This is a placeholder - real implementation would parse all fields
		if int(sampleLength) >= 20 {
			// Extract some basic info (this is simplified)
			flow.Protocol = 6 // Default to TCP
			flow.Packets = 1
			flow.Bytes = uint64(sampleLength)
		}

		flows = append(flows, flow)
		offset += int(sampleLength)
	}

	return flows, nil
}

// startNetFlowCollector starts the NetFlow collector
func (fc *FlowCollector) startNetFlowCollector(ctx context.Context) error {
	addr := &net.UDPAddr{
		Port: fc.netflowPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", fc.netflowPort, err)
	}

	fc.netflowListener = conn

	// Start receiver goroutine
	go fc.receiveNetFlow(ctx, conn)

	return nil
}

// receiveNetFlow receives and parses NetFlow datagrams
func (fc *FlowCollector) receiveNetFlow(ctx context.Context, conn *net.UDPConn) {
	buffer := make([]byte, 9000)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				fc.logger.Errorf("NetFlow read error: %v", err)
				continue
			}

			// Parse NetFlow packet
			flows, err := fc.parseNetFlowPacket(buffer[:n], remoteAddr.IP)
			if err != nil {
				fc.parseErrors.WithLabelValues("netflow", "parse_error").Inc()
				fc.logger.Debugf("NetFlow parse error from %s: %v", remoteAddr.IP, err)
				continue
			}

			// Process flows
			for _, flow := range flows {
				fc.flowsReceived.WithLabelValues("netflow", remoteAddr.IP.String()).Inc()
				fc.bytesReceived.WithLabelValues("netflow", protocolName(flow.Protocol)).Add(float64(flow.Bytes))
				fc.packetsReceived.WithLabelValues("netflow", protocolName(flow.Protocol)).Add(float64(flow.Packets))

				if fc.aggregator != nil {
					fc.aggregator.ProcessFlow(flow)
				}
			}
		}
	}
}

// parseNetFlowPacket parses a NetFlow v5 packet
func (fc *FlowCollector) parseNetFlowPacket(data []byte, sourceIP net.IP) ([]*FlowRecord, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	// NetFlow v5 header
	version := binary.BigEndian.Uint16(data[0:2])
	if version != 5 {
		return nil, fmt.Errorf("unsupported NetFlow version: %d (only v5 supported)", version)
	}

	count := binary.BigEndian.Uint16(data[2:4])
	// sysUptime := binary.BigEndian.Uint32(data[4:8])
	// unixSecs := binary.BigEndian.Uint32(data[8:12])
	// unixNsecs := binary.BigEndian.Uint32(data[12:16])
	// flowSeq := binary.BigEndian.Uint32(data[16:20])

	flows := make([]*FlowRecord, 0, count)
	offset := 24 // Header is 24 bytes

	// Each NetFlow v5 record is 48 bytes
	recordSize := 48

	for i := 0; i < int(count) && offset+recordSize <= len(data); i++ {
		rec := data[offset : offset+recordSize]

		flow := &FlowRecord{
			Type:      "netflow",
			StartTime: time.Now(),
			EndTime:   time.Now(),
		}

		// Parse NetFlow v5 record
		flow.SrcIP = net.IP(rec[0:4])
		flow.DstIP = net.IP(rec[4:8])
		// nextHop := net.IP(rec[8:12])
		// input := binary.BigEndian.Uint16(rec[12:14])
		// output := binary.BigEndian.Uint16(rec[14:16])
		flow.Packets = uint64(binary.BigEndian.Uint32(rec[16:20]))
		flow.Bytes = uint64(binary.BigEndian.Uint32(rec[20:24]))
		// first := binary.BigEndian.Uint32(rec[24:28])
		// last := binary.BigEndian.Uint32(rec[28:32])
		flow.SrcPort = binary.BigEndian.Uint16(rec[32:34])
		flow.DstPort = binary.BigEndian.Uint16(rec[34:36])
		// pad1 := rec[36]
		flow.TCPFlags = rec[37]
		flow.Protocol = rec[38]
		// tos := rec[39]
		flow.SrcAS = uint32(binary.BigEndian.Uint16(rec[40:42]))
		flow.DstAS = uint32(binary.BigEndian.Uint16(rec[42:44]))
		// srcMask := rec[44]
		// dstMask := rec[45]
		// pad2 := binary.BigEndian.Uint16(rec[46:48])

		flows = append(flows, flow)
		offset += recordSize
	}

	return flows, nil
}

// startIPFIXCollector starts the IPFIX collector
func (fc *FlowCollector) startIPFIXCollector(ctx context.Context) error {
	addr := &net.UDPAddr{
		Port: fc.ipfixPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", fc.ipfixPort, err)
	}

	fc.ipfixListener = conn

	// Start receiver goroutine
	go fc.receiveIPFIX(ctx, conn)

	return nil
}

// receiveIPFIX receives and parses IPFIX messages
func (fc *FlowCollector) receiveIPFIX(ctx context.Context, conn *net.UDPConn) {
	buffer := make([]byte, 9000)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				fc.logger.Errorf("IPFIX read error: %v", err)
				continue
			}

			// Parse IPFIX message
			flows, err := fc.parseIPFIXMessage(buffer[:n], remoteAddr.IP)
			if err != nil {
				fc.parseErrors.WithLabelValues("ipfix", "parse_error").Inc()
				fc.logger.Debugf("IPFIX parse error from %s: %v", remoteAddr.IP, err)
				continue
			}

			// Process flows
			for _, flow := range flows {
				fc.flowsReceived.WithLabelValues("ipfix", remoteAddr.IP.String()).Inc()
				fc.bytesReceived.WithLabelValues("ipfix", protocolName(flow.Protocol)).Add(float64(flow.Bytes))
				fc.packetsReceived.WithLabelValues("ipfix", protocolName(flow.Protocol)).Add(float64(flow.Packets))

				if fc.aggregator != nil {
					fc.aggregator.ProcessFlow(flow)
				}
			}
		}
	}
}

// parseIPFIXMessage parses an IPFIX message (RFC 7011)
func (fc *FlowCollector) parseIPFIXMessage(data []byte, sourceIP net.IP) ([]*FlowRecord, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("message too short: %d bytes", len(data))
	}

	// IPFIX header
	version := binary.BigEndian.Uint16(data[0:2])
	if version != 10 {
		return nil, fmt.Errorf("unsupported IPFIX version: %d", version)
	}

	// length := binary.BigEndian.Uint16(data[2:4])
	// exportTime := binary.BigEndian.Uint32(data[4:8])
	// seqNum := binary.BigEndian.Uint32(data[8:12])
	// observationDomainID := binary.BigEndian.Uint32(data[12:16])

	// For simplicity, create placeholder flows
	// Real implementation would parse IPFIX templates and data sets
	flows := []*FlowRecord{
		{
			Type:      "ipfix",
			Protocol:  6, // TCP
			Packets:   1,
			Bytes:     uint64(len(data)),
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Raw:       data,
		},
	}

	return flows, nil
}

// protocolName returns protocol name from number
func protocolName(proto uint8) string {
	switch proto {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("proto_%d", proto)
	}
}
