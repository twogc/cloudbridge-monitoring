/*
CloudBridge Monitoring - Flow Aggregator
Aggregates flow records for anomaly detection and traffic analysis
*/

package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// FlowAggregator aggregates flow records for analysis
type FlowAggregator struct {
	logger *logrus.Logger

	// Aggregation windows
	trafficByIP    map[string]*IPTrafficStats   // Key: IP address
	trafficByPort  map[uint16]*PortTrafficStats // Key: port
	trafficByProto map[uint8]*ProtoTrafficStats // Key: protocol

	// Top talkers
	topSources      []*TopTalker
	topDestinations []*TopTalker

	// Anomaly detection
	baselineTraffic map[string]*TrafficBaseline

	// Time windows
	windowDuration time.Duration
	lastReset      time.Time

	// Metrics
	totalFlows      prometheus.Counter
	totalBytes      prometheus.Counter
	totalPackets    prometheus.Counter
	flowRate        prometheus.Gauge
	trafficRate     prometheus.Gauge
	topTalkersGauge *prometheus.GaugeVec

	// Mutex
	mu sync.RWMutex
}

// IPTrafficStats tracks traffic statistics per IP
type IPTrafficStats struct {
	IP              string
	BytesSent       uint64
	BytesReceived   uint64
	PacketsSent     uint64
	PacketsReceived uint64
	FlowsSent       uint64
	FlowsReceived   uint64
	LastSeen        time.Time

	// Protocol breakdown
	TCPBytes  uint64
	UDPBytes  uint64
	ICMPBytes uint64

	// Flags
	TCPSyn uint64
	TCPAck uint64
	TCPRst uint64
	TCPFin uint64
}

// PortTrafficStats tracks traffic statistics per port
type PortTrafficStats struct {
	Port      uint16
	Bytes     uint64
	Packets   uint64
	Flows     uint64
	UniqueIPs map[string]bool
	LastSeen  time.Time
}

// ProtoTrafficStats tracks traffic statistics per protocol
type ProtoTrafficStats struct {
	Protocol uint8
	Bytes    uint64
	Packets  uint64
	Flows    uint64
	LastSeen time.Time
}

// TopTalker represents a top traffic source/destination
type TopTalker struct {
	IP      string
	Bytes   uint64
	Packets uint64
	Flows   uint64
	Rank    int
}

// TrafficBaseline represents baseline traffic for anomaly detection
type TrafficBaseline struct {
	IP             string
	AvgBytesPerSec float64
	StdDevBytes    float64
	AvgPktsPerSec  float64
	StdDevPkts     float64
	Samples        int
	LastUpdate     time.Time
}

// talkerCandidate represents a candidate for top talker
type talkerCandidate struct {
	ip      string
	bytes   uint64
	packets uint64
	flows   uint64
}

// NewFlowAggregator creates a new flow aggregator
func NewFlowAggregator(windowDuration time.Duration) *FlowAggregator {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	fa := &FlowAggregator{
		logger:          logger,
		trafficByIP:     make(map[string]*IPTrafficStats),
		trafficByPort:   make(map[uint16]*PortTrafficStats),
		trafficByProto:  make(map[uint8]*ProtoTrafficStats),
		topSources:      make([]*TopTalker, 0, 10),
		topDestinations: make([]*TopTalker, 0, 10),
		baselineTraffic: make(map[string]*TrafficBaseline),
		windowDuration:  windowDuration,
		lastReset:       time.Now(),
	}

	// Initialize metrics
	fa.initMetrics()

	// Start background tasks
	go fa.aggregationLoop()

	return fa
}

// initMetrics initializes Prometheus metrics
func (fa *FlowAggregator) initMetrics() {
	fa.totalFlows = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_flow_aggregator_flows_total",
		Help: "Total number of flows processed",
	})

	fa.totalBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_flow_aggregator_bytes_total",
		Help: "Total bytes processed",
	})

	fa.totalPackets = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_flow_aggregator_packets_total",
		Help: "Total packets processed",
	})

	fa.flowRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cloudbridge_flow_aggregator_flow_rate",
		Help: "Current flow rate (flows per second)",
	})

	fa.trafficRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cloudbridge_flow_aggregator_traffic_rate_bps",
		Help: "Current traffic rate (bits per second)",
	})

	fa.topTalkersGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudbridge_flow_aggregator_top_talkers_bps",
			Help: "Top talkers traffic rate",
		},
		[]string{"ip", "direction", "rank"},
	)

	prometheus.MustRegister(fa.totalFlows)
	prometheus.MustRegister(fa.totalBytes)
	prometheus.MustRegister(fa.totalPackets)
	prometheus.MustRegister(fa.flowRate)
	prometheus.MustRegister(fa.trafficRate)
	prometheus.MustRegister(fa.topTalkersGauge)
}

// ProcessFlow processes a flow record
func (fa *FlowAggregator) ProcessFlow(flow *FlowRecord) {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	// Update global counters
	fa.totalFlows.Inc()
	fa.totalBytes.Add(float64(flow.Bytes))
	fa.totalPackets.Add(float64(flow.Packets))

	// Update source IP stats
	if flow.SrcIP != nil {
		srcIP := flow.SrcIP.String()
		stats, exists := fa.trafficByIP[srcIP]
		if !exists {
			stats = &IPTrafficStats{
				IP: srcIP,
			}
			fa.trafficByIP[srcIP] = stats
		}

		stats.BytesSent += flow.Bytes
		stats.PacketsSent += flow.Packets
		stats.FlowsSent++
		stats.LastSeen = time.Now()

		// Update protocol counters
		switch flow.Protocol {
		case 6: // TCP
			stats.TCPBytes += flow.Bytes
			// Parse TCP flags
			if flow.TCPFlags&0x02 != 0 {
				stats.TCPSyn++
			}
			if flow.TCPFlags&0x10 != 0 {
				stats.TCPAck++
			}
			if flow.TCPFlags&0x04 != 0 {
				stats.TCPRst++
			}
			if flow.TCPFlags&0x01 != 0 {
				stats.TCPFin++
			}
		case 17: // UDP
			stats.UDPBytes += flow.Bytes
		case 1: // ICMP
			stats.ICMPBytes += flow.Bytes
		}
	}

	// Update destination IP stats
	if flow.DstIP != nil {
		dstIP := flow.DstIP.String()
		stats, exists := fa.trafficByIP[dstIP]
		if !exists {
			stats = &IPTrafficStats{
				IP: dstIP,
			}
			fa.trafficByIP[dstIP] = stats
		}

		stats.BytesReceived += flow.Bytes
		stats.PacketsReceived += flow.Packets
		stats.FlowsReceived++
		stats.LastSeen = time.Now()
	}

	// Update port stats
	if flow.DstPort > 0 {
		portStats, exists := fa.trafficByPort[flow.DstPort]
		if !exists {
			portStats = &PortTrafficStats{
				Port:      flow.DstPort,
				UniqueIPs: make(map[string]bool),
			}
			fa.trafficByPort[flow.DstPort] = portStats
		}

		portStats.Bytes += flow.Bytes
		portStats.Packets += flow.Packets
		portStats.Flows++
		portStats.LastSeen = time.Now()

		if flow.SrcIP != nil {
			portStats.UniqueIPs[flow.SrcIP.String()] = true
		}
	}

	// Update protocol stats
	protoStats, exists := fa.trafficByProto[flow.Protocol]
	if !exists {
		protoStats = &ProtoTrafficStats{
			Protocol: flow.Protocol,
		}
		fa.trafficByProto[flow.Protocol] = protoStats
	}

	protoStats.Bytes += flow.Bytes
	protoStats.Packets += flow.Packets
	protoStats.Flows++
	protoStats.LastSeen = time.Now()
}

// aggregationLoop performs periodic aggregation tasks
func (fa *FlowAggregator) aggregationLoop() {
	ticker := time.NewTicker(fa.windowDuration)
	defer ticker.Stop()

	for range ticker.C {
		fa.updateTopTalkers()
		fa.updateBaselines()
		fa.updateMetrics()
		fa.cleanupOldData()
	}
}

// updateTopTalkers updates the list of top talkers
func (fa *FlowAggregator) updateTopTalkers() {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	// Find top sources by bytes sent
	sources := make([]talkerCandidate, 0, len(fa.trafficByIP))
	destinations := make([]talkerCandidate, 0, len(fa.trafficByIP))

	for ip, stats := range fa.trafficByIP {
		sources = append(sources, talkerCandidate{
			ip:      ip,
			bytes:   stats.BytesSent,
			packets: stats.PacketsSent,
			flows:   stats.FlowsSent,
		})

		destinations = append(destinations, talkerCandidate{
			ip:      ip,
			bytes:   stats.BytesReceived,
			packets: stats.PacketsReceived,
			flows:   stats.FlowsReceived,
		})
	}

	// Sort and take top 10
	fa.topSources = fa.getTopN(sources, 10)
	fa.topDestinations = fa.getTopN(destinations, 10)

	// Update Prometheus metrics
	for _, talker := range fa.topSources {
		bytesPerSec := float64(talker.Bytes) / fa.windowDuration.Seconds()
		fa.topTalkersGauge.WithLabelValues(
			talker.IP,
			"source",
			fmt.Sprintf("%d", talker.Rank),
		).Set(bytesPerSec * 8) // Convert to bits per second
	}

	for _, talker := range fa.topDestinations {
		bytesPerSec := float64(talker.Bytes) / fa.windowDuration.Seconds()
		fa.topTalkersGauge.WithLabelValues(
			talker.IP,
			"destination",
			fmt.Sprintf("%d", talker.Rank),
		).Set(bytesPerSec * 8)
	}
}

// getTopN returns top N talkers sorted by bytes
func (fa *FlowAggregator) getTopN(candidates []talkerCandidate, n int) []*TopTalker {
	// Simple bubble sort for top N (good enough for small N)
	for i := 0; i < len(candidates)-1; i++ {
		for j := 0; j < len(candidates)-i-1; j++ {
			if candidates[j].bytes < candidates[j+1].bytes {
				candidates[j], candidates[j+1] = candidates[j+1], candidates[j]
			}
		}
	}

	// Take top N
	topN := make([]*TopTalker, 0, n)
	for i := 0; i < n && i < len(candidates); i++ {
		if candidates[i].bytes == 0 {
			continue
		}
		topN = append(topN, &TopTalker{
			IP:      candidates[i].ip,
			Bytes:   candidates[i].bytes,
			Packets: candidates[i].packets,
			Flows:   candidates[i].flows,
			Rank:    i + 1,
		})
	}

	return topN
}

// updateBaselines updates traffic baselines for anomaly detection
func (fa *FlowAggregator) updateBaselines() {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	now := time.Now()
	windowSec := fa.windowDuration.Seconds()

	for ip, stats := range fa.trafficByIP {
		baseline, exists := fa.baselineTraffic[ip]
		if !exists {
			baseline = &TrafficBaseline{
				IP: ip,
			}
			fa.baselineTraffic[ip] = baseline
		}

		// Calculate current rates
		currentBytesPerSec := float64(stats.BytesSent+stats.BytesReceived) / windowSec
		currentPktsPerSec := float64(stats.PacketsSent+stats.PacketsReceived) / windowSec

		// Update moving average (exponential weighted moving average)
		alpha := 0.1 // Smoothing factor
		if baseline.Samples == 0 {
			baseline.AvgBytesPerSec = currentBytesPerSec
			baseline.AvgPktsPerSec = currentPktsPerSec
		} else {
			baseline.AvgBytesPerSec = alpha*currentBytesPerSec + (1-alpha)*baseline.AvgBytesPerSec
			baseline.AvgPktsPerSec = alpha*currentPktsPerSec + (1-alpha)*baseline.AvgPktsPerSec

			// Update standard deviation (simplified)
			byteDiff := currentBytesPerSec - baseline.AvgBytesPerSec
			pktDiff := currentPktsPerSec - baseline.AvgPktsPerSec

			baseline.StdDevBytes = alpha*abs(byteDiff) + (1-alpha)*baseline.StdDevBytes
			baseline.StdDevPkts = alpha*abs(pktDiff) + (1-alpha)*baseline.StdDevPkts
		}

		baseline.Samples++
		baseline.LastUpdate = now
	}
}

// DetectAnomalies detects traffic anomalies
func (fa *FlowAggregator) DetectAnomalies(thresholdStdDev float64) []Anomaly {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	anomalies := make([]Anomaly, 0)
	windowSec := fa.windowDuration.Seconds()

	for ip, stats := range fa.trafficByIP {
		baseline, exists := fa.baselineTraffic[ip]
		if !exists || baseline.Samples < 10 {
			// Not enough data for baseline
			continue
		}

		// Calculate current rates
		currentBytesPerSec := float64(stats.BytesSent+stats.BytesReceived) / windowSec
		currentPktsPerSec := float64(stats.PacketsSent+stats.PacketsReceived) / windowSec

		// Check for anomalies (Z-score)
		if baseline.StdDevBytes > 0 {
			zScore := (currentBytesPerSec - baseline.AvgBytesPerSec) / baseline.StdDevBytes
			if abs(zScore) > thresholdStdDev {
				anomalies = append(anomalies, Anomaly{
					IP:            ip,
					Type:          "traffic_volume",
					Metric:        "bytes_per_second",
					CurrentValue:  currentBytesPerSec,
					BaselineValue: baseline.AvgBytesPerSec,
					StdDev:        baseline.StdDevBytes,
					ZScore:        zScore,
					Severity:      calculateSeverity(abs(zScore)),
					DetectedAt:    time.Now(),
				})
			}
		}

		if baseline.StdDevPkts > 0 {
			zScore := (currentPktsPerSec - baseline.AvgPktsPerSec) / baseline.StdDevPkts
			if abs(zScore) > thresholdStdDev {
				anomalies = append(anomalies, Anomaly{
					IP:            ip,
					Type:          "packet_rate",
					Metric:        "packets_per_second",
					CurrentValue:  currentPktsPerSec,
					BaselineValue: baseline.AvgPktsPerSec,
					StdDev:        baseline.StdDevPkts,
					ZScore:        zScore,
					Severity:      calculateSeverity(abs(zScore)),
					DetectedAt:    time.Now(),
				})
			}
		}
	}

	return anomalies
}

// Anomaly represents a detected traffic anomaly
type Anomaly struct {
	IP            string
	Type          string // traffic_volume, packet_rate, etc.
	Metric        string
	CurrentValue  float64
	BaselineValue float64
	StdDev        float64
	ZScore        float64
	Severity      string // low, medium, high, critical
	DetectedAt    time.Time
}

// calculateSeverity calculates severity from Z-score
func calculateSeverity(zScore float64) string {
	if zScore > 5 {
		return "critical"
	} else if zScore > 3 {
		return "high"
	} else if zScore > 2 {
		return "medium"
	}
	return "low"
}

// updateMetrics updates Prometheus metrics
func (fa *FlowAggregator) updateMetrics() {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	// Calculate rates
	windowSec := fa.windowDuration.Seconds()
	var totalBytes uint64
	var totalFlows uint64

	for _, stats := range fa.trafficByIP {
		totalBytes += stats.BytesSent + stats.BytesReceived
		totalFlows += stats.FlowsSent + stats.FlowsReceived
	}

	flowRate := float64(totalFlows) / windowSec
	trafficRate := float64(totalBytes) * 8 / windowSec // bits per second

	fa.flowRate.Set(flowRate)
	fa.trafficRate.Set(trafficRate)
}

// cleanupOldData removes old data outside the window
func (fa *FlowAggregator) cleanupOldData() {
	fa.mu.Lock()
	defer fa.mu.Unlock()

	now := time.Now()
	cleanupAge := fa.windowDuration * 2

	// Cleanup IP stats
	for ip, stats := range fa.trafficByIP {
		if now.Sub(stats.LastSeen) > cleanupAge {
			delete(fa.trafficByIP, ip)
		}
	}

	// Cleanup port stats
	for port, stats := range fa.trafficByPort {
		if now.Sub(stats.LastSeen) > cleanupAge {
			delete(fa.trafficByPort, port)
		}
	}

	// Cleanup protocol stats
	for proto, stats := range fa.trafficByProto {
		if now.Sub(stats.LastSeen) > cleanupAge {
			delete(fa.trafficByProto, proto)
		}
	}

	// Cleanup baselines older than 1 hour
	for ip, baseline := range fa.baselineTraffic {
		if now.Sub(baseline.LastUpdate) > 1*time.Hour {
			delete(fa.baselineTraffic, ip)
		}
	}
}

// GetTopTalkers returns current top talkers
func (fa *FlowAggregator) GetTopTalkers() (sources, destinations []*TopTalker) {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	return fa.topSources, fa.topDestinations
}

// GetIPStats returns statistics for a specific IP
func (fa *FlowAggregator) GetIPStats(ip string) (*IPTrafficStats, error) {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	stats, exists := fa.trafficByIP[ip]
	if !exists {
		return nil, fmt.Errorf("no stats for IP %s", ip)
	}

	return stats, nil
}

// GetPortStats returns statistics for a specific port
func (fa *FlowAggregator) GetPortStats(port uint16) (*PortTrafficStats, error) {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	stats, exists := fa.trafficByPort[port]
	if !exists {
		return nil, fmt.Errorf("no stats for port %d", port)
	}

	return stats, nil
}

// GetProtocolStats returns all protocol statistics
func (fa *FlowAggregator) GetProtocolStats() map[uint8]*ProtoTrafficStats {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	// Return copy
	statsCopy := make(map[uint8]*ProtoTrafficStats)
	for proto, stats := range fa.trafficByProto {
		statsCopy[proto] = stats
	}

	return statsCopy
}

// abs returns absolute value of float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// IsAttackPattern detects potential attack patterns
func (fa *FlowAggregator) IsAttackPattern(ip string) (bool, string) {
	fa.mu.RLock()
	defer fa.mu.RUnlock()

	stats, exists := fa.trafficByIP[ip]
	if !exists {
		return false, ""
	}

	// Heuristic 1: High SYN rate (SYN flood)
	if stats.TCPSyn > 1000 && stats.TCPAck < stats.TCPSyn/10 {
		return true, "SYN flood"
	}

	// Heuristic 2: High RST rate
	if stats.TCPRst > 500 {
		return true, "TCP RST flood"
	}

	// Heuristic 3: UDP flood
	if stats.UDPBytes > stats.TCPBytes*10 && stats.UDPBytes > 100*1024*1024 {
		return true, "UDP flood"
	}

	// Heuristic 4: ICMP flood
	if stats.ICMPBytes > 50*1024*1024 {
		return true, "ICMP flood"
	}

	return false, ""
}

// Helper to check if IP is valid
func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}
