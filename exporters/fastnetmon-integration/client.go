/*
CloudBridge Monitoring - FastNetMon Client
Integration with FastNetMon for DDoS detection
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// FastNetMonClient integrates with FastNetMon Advanced
type FastNetMonClient struct {
	logger *logrus.Logger

	// Configuration
	apiEndpoint string
	apiKey      string
	apiSecret   string

	// HTTP client
	httpClient *http.Client

	// Active attacks tracking
	activeAttacks map[string]*AttackInfo
	mu            sync.RWMutex

	// Callbacks
	onAttackStart func(*AttackInfo) error
	onAttackStop  func(*AttackInfo) error

	// Metrics
	attacksDetected   prometheus.Counter
	attacksActive     prometheus.Gauge
	attacksMitigated  prometheus.Counter
	falsePositives    prometheus.Counter
	detectionLatency  prometheus.Histogram
}

// AttackInfo represents information about a DDoS attack
type AttackInfo struct {
	// Attack identification
	AttackID     string    `json:"attack_id"`
	TargetIP     string    `json:"target_ip"`
	AttackType   string    `json:"attack_type"`
	AttackVector string    `json:"attack_vector"`

	// Timing
	StartTime time.Time `json:"start_time"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Duration  time.Duration `json:"duration"`

	// Traffic metrics
	TotalTraffic     uint64  `json:"total_traffic"`      // bytes
	TotalPackets     uint64  `json:"total_packets"`
	PeakTraffic      uint64  `json:"peak_traffic"`       // bps
	PeakPackets      uint64  `json:"peak_packets"`       // pps

	// Protocol breakdown
	TCPTraffic   uint64  `json:"tcp_traffic"`
	UDPTraffic   uint64  `json:"udp_traffic"`
	ICMPTraffic  uint64  `json:"icmp_traffic"`

	// Severity
	Severity     string  `json:"severity"`              // low, medium, high, critical
	Confidence   float64 `json:"confidence"`            // 0.0-1.0

	// Source information
	SourceIPs    []string `json:"source_ips,omitempty"`
	SourceASNs   []uint32 `json:"source_asns,omitempty"`
	SourcePorts  []uint16 `json:"source_ports,omitempty"`

	// Mitigation
	MitigationID     string  `json:"mitigation_id,omitempty"`
	MitigationStatus string  `json:"mitigation_status,omitempty"`
	MitigationMethod string  `json:"mitigation_method,omitempty"`

	// Metadata
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// FastNetMonConfig represents FastNetMon configuration
type FastNetMonConfig struct {
	APIEndpoint string
	APIKey      string
	APISecret   string
	Timeout     time.Duration
}

// NewFastNetMonClient creates a new FastNetMon client
func NewFastNetMonClient(config *FastNetMonConfig) (*FastNetMonClient, error) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	client := &FastNetMonClient{
		logger:        logger,
		apiEndpoint:   config.APIEndpoint,
		apiKey:        config.APIKey,
		apiSecret:     config.APISecret,
		activeAttacks: make(map[string]*AttackInfo),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}

	// Initialize metrics
	client.initMetrics()

	return client, nil
}

// initMetrics initializes Prometheus metrics
func (fc *FastNetMonClient) initMetrics() {
	fc.attacksDetected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_fastnetmon_attacks_detected_total",
		Help: "Total number of attacks detected by FastNetMon",
	})

	fc.attacksActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cloudbridge_fastnetmon_attacks_active",
		Help: "Number of currently active attacks",
	})

	fc.attacksMitigated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_fastnetmon_attacks_mitigated_total",
		Help: "Total number of attacks mitigated",
	})

	fc.falsePositives = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cloudbridge_fastnetmon_false_positives_total",
		Help: "Total number of false positive detections",
	})

	fc.detectionLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cloudbridge_fastnetmon_detection_latency_seconds",
		Help:    "Time from attack start to detection",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	})

	prometheus.MustRegister(fc.attacksDetected)
	prometheus.MustRegister(fc.attacksActive)
	prometheus.MustRegister(fc.attacksMitigated)
	prometheus.MustRegister(fc.falsePositives)
	prometheus.MustRegister(fc.detectionLatency)
}

// SetAttackHandlers sets callbacks for attack events
func (fc *FastNetMonClient) SetAttackHandlers(onStart, onStop func(*AttackInfo) error) {
	fc.onAttackStart = onStart
	fc.onAttackStop = onStop
}

// Start starts the FastNetMon client
func (fc *FastNetMonClient) Start(ctx context.Context) error {
	fc.logger.Info("Starting FastNetMon client...")

	// Test connection
	if err := fc.HealthCheck(); err != nil {
		return fmt.Errorf("FastNetMon health check failed: %w", err)
	}

	// Start attack monitor
	go fc.monitorAttacks(ctx)

	fc.logger.Info("FastNetMon client started successfully")
	return nil
}

// Stop stops the FastNetMon client
func (fc *FastNetMonClient) Stop() error {
	fc.logger.Info("Stopping FastNetMon client...")
	fc.logger.Info("FastNetMon client stopped")
	return nil
}

// HealthCheck checks if FastNetMon is reachable
func (fc *FastNetMonClient) HealthCheck() error {
	resp, err := fc.makeAPIRequest("GET", "/api/v1/system/info", nil)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("health check failed: %s", resp.Error)
	}

	fc.logger.Info("FastNetMon health check passed")
	return nil
}

// GetAttacks retrieves current attacks from FastNetMon
func (fc *FastNetMonClient) GetAttacks() ([]*AttackInfo, error) {
	resp, err := fc.makeAPIRequest("GET", "/api/v1/attacks", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get attacks: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Error)
	}

	// Parse attacks
	var attacks []*AttackInfo
	if data, ok := resp.Data["attacks"].([]interface{}); ok {
		for _, item := range data {
			if attackData, ok := item.(map[string]interface{}); ok {
				attack := fc.parseAttackInfo(attackData)
				attacks = append(attacks, attack)
			}
		}
	}

	return attacks, nil
}

// GetAttackDetails retrieves detailed information about an attack
func (fc *FastNetMonClient) GetAttackDetails(attackID string) (*AttackInfo, error) {
	endpoint := fmt.Sprintf("/api/v1/attacks/%s", attackID)
	resp, err := fc.makeAPIRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get attack details: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Error)
	}

	attack := fc.parseAttackInfo(resp.Data)
	return attack, nil
}

// UnbanIP removes an IP from ban list (false positive handling)
func (fc *FastNetMonClient) UnbanIP(ip string) error {
	fc.logger.Infof("Unbanning IP %s (false positive)", ip)

	requestBody := map[string]interface{}{
		"ip": ip,
	}

	resp, err := fc.makeAPIRequest("POST", "/api/v1/unban", requestBody)
	if err != nil {
		return fmt.Errorf("failed to unban IP: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("API error: %s", resp.Error)
	}

	fc.falsePositives.Inc()
	fc.logger.Infof("Successfully unbanned IP %s", ip)
	return nil
}

// GetHostGroups retrieves configured host groups
func (fc *FastNetMonClient) GetHostGroups() ([]HostGroup, error) {
	resp, err := fc.makeAPIRequest("GET", "/api/v1/hostgroups", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get host groups: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("API error: %s", resp.Error)
	}

	var groups []HostGroup
	// Parse host groups from response
	// Implementation depends on FastNetMon API structure

	return groups, nil
}

// HostGroup represents a FastNetMon host group
type HostGroup struct {
	Name        string
	Description string
	Networks    []string
	Enabled     bool
}

// monitorAttacks continuously monitors for attacks
func (fc *FastNetMonClient) monitorAttacks(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fc.checkAttacks()
		}
	}
}

// checkAttacks checks for new/updated attacks
func (fc *FastNetMonClient) checkAttacks() {
	attacks, err := fc.GetAttacks()
	if err != nil {
		fc.logger.Errorf("Failed to get attacks: %v", err)
		return
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	// Track current attack IDs
	currentAttacks := make(map[string]bool)

	for _, attack := range attacks {
		currentAttacks[attack.AttackID] = true

		// Check if this is a new attack
		if _, exists := fc.activeAttacks[attack.AttackID]; !exists {
			fc.logger.Infof("New attack detected: %s targeting %s (type: %s)",
				attack.AttackID, attack.TargetIP, attack.AttackType)

			fc.activeAttacks[attack.AttackID] = attack
			fc.attacksDetected.Inc()
			fc.attacksActive.Inc()

			// Calculate detection latency
			latency := time.Since(attack.StartTime).Seconds()
			fc.detectionLatency.Observe(latency)

			// Trigger callback
			if fc.onAttackStart != nil {
				if err := fc.onAttackStart(attack); err != nil {
					fc.logger.Errorf("Attack start callback failed: %v", err)
				}
			}
		} else {
			// Update existing attack
			fc.activeAttacks[attack.AttackID] = attack
		}
	}

	// Check for stopped attacks
	for attackID, attack := range fc.activeAttacks {
		if !currentAttacks[attackID] {
			fc.logger.Infof("Attack stopped: %s targeting %s",
				attackID, attack.TargetIP)

			now := time.Now()
			attack.EndTime = &now
			attack.Duration = now.Sub(attack.StartTime)

			delete(fc.activeAttacks, attackID)
			fc.attacksActive.Dec()
			fc.attacksMitigated.Inc()

			// Trigger callback
			if fc.onAttackStop != nil {
				if err := fc.onAttackStop(attack); err != nil {
					fc.logger.Errorf("Attack stop callback failed: %v", err)
				}
			}
		}
	}
}

// parseAttackInfo parses attack information from API response
func (fc *FastNetMonClient) parseAttackInfo(data map[string]interface{}) *AttackInfo {
	attack := &AttackInfo{
		Metadata: make(map[string]interface{}),
	}

	// Parse fields (simplified - real implementation depends on FastNetMon API)
	if id, ok := data["attack_id"].(string); ok {
		attack.AttackID = id
	}
	if ip, ok := data["target_ip"].(string); ok {
		attack.TargetIP = ip
	}
	if attackType, ok := data["attack_type"].(string); ok {
		attack.AttackType = attackType
	}
	if vector, ok := data["attack_vector"].(string); ok {
		attack.AttackVector = vector
	}

	// Parse traffic metrics
	if traffic, ok := data["total_traffic"].(float64); ok {
		attack.TotalTraffic = uint64(traffic)
	}
	if packets, ok := data["total_packets"].(float64); ok {
		attack.TotalPackets = uint64(packets)
	}
	if peak, ok := data["peak_traffic"].(float64); ok {
		attack.PeakTraffic = uint64(peak)
	}

	// Parse severity
	if severity, ok := data["severity"].(string); ok {
		attack.Severity = severity
	} else {
		attack.Severity = fc.calculateSeverity(attack)
	}

	// Parse confidence
	if confidence, ok := data["confidence"].(float64); ok {
		attack.Confidence = confidence
	} else {
		attack.Confidence = 0.95 // Default high confidence for FastNetMon
	}

	// Parse timestamp
	if startTime, ok := data["start_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			attack.StartTime = t
		}
	}
	if attack.StartTime.IsZero() {
		attack.StartTime = time.Now()
	}

	return attack
}

// calculateSeverity calculates attack severity based on metrics
func (fc *FastNetMonClient) calculateSeverity(attack *AttackInfo) string {
	// Critical: >10 Gbps or >10M pps
	if attack.PeakTraffic > 10_000_000_000 || attack.PeakPackets > 10_000_000 {
		return "critical"
	}

	// High: >1 Gbps or >1M pps
	if attack.PeakTraffic > 1_000_000_000 || attack.PeakPackets > 1_000_000 {
		return "high"
	}

	// Medium: >100 Mbps or >100K pps
	if attack.PeakTraffic > 100_000_000 || attack.PeakPackets > 100_000 {
		return "medium"
	}

	// Low: everything else
	return "low"
}

// GetActiveAttacks returns currently active attacks
func (fc *FastNetMonClient) GetActiveAttacks() []*AttackInfo {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	attacks := make([]*AttackInfo, 0, len(fc.activeAttacks))
	for _, attack := range fc.activeAttacks {
		attacks = append(attacks, attack)
	}

	return attacks
}

// makeAPIRequest makes an API request to FastNetMon
func (fc *FastNetMonClient) makeAPIRequest(method, endpoint string, body interface{}) (*APIResponse, error) {
	url := fc.apiEndpoint + endpoint

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = &bytesReader{data: jsonBody}
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", fc.apiKey)

	// Execute request
	resp, err := fc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		// Try to create a generic response
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return &APIResponse{
				Success: true,
				Data:    make(map[string]interface{}),
			}, nil
		}
		return &APIResponse{
			Success: false,
			Error:   string(respBody),
		}, nil
	}

	if resp.StatusCode >= 400 {
		apiResp.Success = false
		if apiResp.Error == "" {
			apiResp.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)
		}
	}

	return &apiResp, nil
}

// APIResponse represents a FastNetMon API response
type APIResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error,omitempty"`
}

// bytesReader wraps []byte for io.Reader
type bytesReader struct {
	data []byte
	pos  int
}

func (br *bytesReader) Read(p []byte) (n int, err error) {
	if br.pos >= len(br.data) {
		return 0, io.EOF
	}
	n = copy(p, br.data[br.pos:])
	br.pos += n
	return n, nil
}

// IsValidIP checks if string is valid IP
func IsValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}
