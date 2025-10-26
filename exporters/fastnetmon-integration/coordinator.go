/*
CloudBridge Monitoring - FastNetMon Mitigation Coordinator
Coordinates FastNetMon detection with CloudBridge mitigation systems
*/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// DefaultMitigationCoordinator coordinates mitigation actions
type DefaultMitigationCoordinator struct {
	logger *logrus.Logger

	// Scrubbing manager endpoint
	scrubbingEndpoint string

	// BGP manager endpoint (for RTBH)
	bgpEndpoint string

	// HTTP client
	httpClient *http.Client

	// Configuration
	config *CoordinatorConfig
}

// CoordinatorConfig represents coordinator configuration
type CoordinatorConfig struct {
	// Mitigation strategy selection
	AutoSelectStrategy bool

	// Thresholds for strategy selection
	RTBHThresholdBps      uint64  // Trigger RTBH above this bps
	ScrubbingThresholdBps uint64  // Trigger scrubbing above this bps

	// Confidence thresholds
	MinConfidence float64 // Minimum confidence to trigger mitigation

	// Automatic approval settings
	AutoApprove          bool
	AutoApproveThreshold float64  // Auto-approve if confidence above this

	// Default mitigation duration
	DefaultDuration time.Duration
}

// NewDefaultMitigationCoordinator creates a new coordinator
func NewDefaultMitigationCoordinator(scrubbingEndpoint, bgpEndpoint string) *DefaultMitigationCoordinator {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	return &DefaultMitigationCoordinator{
		logger:            logger,
		scrubbingEndpoint: scrubbingEndpoint,
		bgpEndpoint:       bgpEndpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config: &CoordinatorConfig{
			AutoSelectStrategy:    true,
			RTBHThresholdBps:      5_000_000_000,   // 5 Gbps
			ScrubbingThresholdBps: 10_000_000_000,  // 10 Gbps
			MinConfidence:         0.85,
			AutoApprove:           false,  // Require human approval by default
			AutoApproveThreshold:  0.95,
			DefaultDuration:       1 * time.Hour,
		},
	}
}

// SetConfig sets coordinator configuration
func (dmc *DefaultMitigationCoordinator) SetConfig(config *CoordinatorConfig) {
	dmc.config = config
}

// TriggerMitigation triggers mitigation for an attack
func (dmc *DefaultMitigationCoordinator) TriggerMitigation(ctx context.Context, attack *AttackInfo) (*MitigationInfo, error) {
	dmc.logger.Infof("Triggering mitigation for attack %s targeting %s", attack.AttackID, attack.TargetIP)

	// Check confidence threshold
	if attack.Confidence < dmc.config.MinConfidence {
		return nil, fmt.Errorf("attack confidence %.2f below minimum %.2f",
			attack.Confidence, dmc.config.MinConfidence)
	}

	// Determine mitigation strategy
	strategy := dmc.selectStrategy(attack)
	dmc.logger.Infof("Selected mitigation strategy: %s", strategy)

	// Create mitigation info
	mitigation := &MitigationInfo{
		ID:               fmt.Sprintf("mitigation-%s", attack.AttackID),
		AttackID:         attack.AttackID,
		TargetIP:         attack.TargetIP,
		Strategy:         strategy,
		Status:           "pending",
		StartedAt:        time.Now(),
		MitigationMethod: "fastnetmon-auto",
		Metadata: map[string]interface{}{
			"attack_type":   attack.AttackType,
			"attack_vector": attack.AttackVector,
			"severity":      attack.Severity,
			"confidence":    attack.Confidence,
		},
	}

	// Execute mitigation based on strategy
	switch strategy {
	case "local":
		// Local mitigation via XDP/nftables (already handled by edge nodes)
		mitigation.Status = "active"
		dmc.logger.Info("Local mitigation active (handled by edge nodes)")

	case "rtbh":
		// Trigger RTBH via BGP manager
		if err := dmc.triggerRTBH(ctx, attack, mitigation); err != nil {
			mitigation.Status = "failed"
			return mitigation, fmt.Errorf("RTBH failed: %w", err)
		}
		mitigation.Status = "active"

	case "scrubbing":
		// Trigger upstream scrubbing
		if err := dmc.triggerScrubbing(ctx, attack, mitigation); err != nil {
			mitigation.Status = "failed"
			return mitigation, fmt.Errorf("scrubbing failed: %w", err)
		}
		mitigation.Status = "active"

	case "hybrid":
		// Trigger both RTBH and scrubbing
		if err := dmc.triggerRTBH(ctx, attack, mitigation); err != nil {
			dmc.logger.Errorf("RTBH failed in hybrid mode: %v", err)
		}
		if err := dmc.triggerScrubbing(ctx, attack, mitigation); err != nil {
			dmc.logger.Errorf("Scrubbing failed in hybrid mode: %v", err)
		}
		mitigation.Status = "active"

	default:
		return mitigation, fmt.Errorf("unknown mitigation strategy: %s", strategy)
	}

	dmc.logger.Infof("Mitigation %s active for attack %s", mitigation.ID, attack.AttackID)
	return mitigation, nil
}

// StopMitigation stops a mitigation
func (dmc *DefaultMitigationCoordinator) StopMitigation(ctx context.Context, mitigationID string) error {
	dmc.logger.Infof("Stopping mitigation %s", mitigationID)

	// Call scrubbing manager to stop
	if dmc.scrubbingEndpoint != "" {
		endpoint := fmt.Sprintf("%s/api/v1/mitigation/stop", dmc.scrubbingEndpoint)
		reqBody := map[string]interface{}{
			"mitigation_id": mitigationID,
		}

		if err := dmc.makeAPIRequest(ctx, "POST", endpoint, reqBody); err != nil {
			dmc.logger.Errorf("Failed to stop scrubbing: %v", err)
		}
	}

	// Call BGP manager to withdraw RTBH
	if dmc.bgpEndpoint != "" {
		endpoint := fmt.Sprintf("%s/api/v1/rtbh/withdraw", dmc.bgpEndpoint)
		reqBody := map[string]interface{}{
			"mitigation_id": mitigationID,
		}

		if err := dmc.makeAPIRequest(ctx, "POST", endpoint, reqBody); err != nil {
			dmc.logger.Errorf("Failed to withdraw RTBH: %v", err)
		}
	}

	dmc.logger.Infof("Mitigation %s stopped", mitigationID)
	return nil
}

// GetMitigationStatus gets mitigation status
func (dmc *DefaultMitigationCoordinator) GetMitigationStatus(ctx context.Context, mitigationID string) (*MitigationInfo, error) {
	// Query scrubbing manager
	if dmc.scrubbingEndpoint != "" {
		endpoint := fmt.Sprintf("%s/api/v1/mitigations/%s", dmc.scrubbingEndpoint, mitigationID)

		resp, err := dmc.httpClient.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get mitigation status: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var mitigation MitigationInfo
			if err := json.NewDecoder(resp.Body).Decode(&mitigation); err != nil {
				return nil, fmt.Errorf("failed to parse response: %w", err)
			}
			return &mitigation, nil
		}
	}

	return nil, fmt.Errorf("mitigation %s not found", mitigationID)
}

// selectStrategy selects appropriate mitigation strategy
func (dmc *DefaultMitigationCoordinator) selectStrategy(attack *AttackInfo) string {
	if !dmc.config.AutoSelectStrategy {
		// Default to scrubbing if no auto-select
		return "scrubbing"
	}

	// Strategy selection logic:

	// 1. Very high volume (>10 Gbps) -> Scrubbing
	if attack.PeakTraffic > dmc.config.ScrubbingThresholdBps {
		return "scrubbing"
	}

	// 2. High volume (>5 Gbps) + high confidence -> RTBH
	if attack.PeakTraffic > dmc.config.RTBHThresholdBps && attack.Confidence >= 0.95 {
		return "rtbh"
	}

	// 3. Medium volume + critical severity -> Hybrid
	if attack.PeakTraffic > 1_000_000_000 && attack.Severity == "critical" {
		return "hybrid"
	}

	// 4. Application layer attacks -> Local
	if attack.AttackType == "application" || attack.AttackType == "http_flood" {
		return "local"
	}

	// 5. Low volume -> Local
	if attack.PeakTraffic < 500_000_000 {
		return "local"
	}

	// 6. Default: Scrubbing for safety
	return "scrubbing"
}

// triggerRTBH triggers RTBH mitigation
func (dmc *DefaultMitigationCoordinator) triggerRTBH(ctx context.Context, attack *AttackInfo, mitigation *MitigationInfo) error {
	dmc.logger.Infof("Triggering RTBH for %s", attack.TargetIP)

	if dmc.bgpEndpoint == "" {
		return fmt.Errorf("BGP endpoint not configured")
	}

	endpoint := fmt.Sprintf("%s/api/v1/rtbh/apply", dmc.bgpEndpoint)
	reqBody := map[string]interface{}{
		"prefix":        attack.TargetIP + "/32",
		"reason":        fmt.Sprintf("FastNetMon: %s attack", attack.AttackType),
		"duration":      int(dmc.config.DefaultDuration.Seconds()),
		"mitigation_id": mitigation.ID,
		"attack_id":     attack.AttackID,
	}

	if err := dmc.makeAPIRequest(ctx, "POST", endpoint, reqBody); err != nil {
		return fmt.Errorf("RTBH API call failed: %w", err)
	}

	mitigation.Metadata["rtbh_applied"] = true
	dmc.logger.Infof("RTBH applied for %s", attack.TargetIP)
	return nil
}

// triggerScrubbing triggers upstream scrubbing
func (dmc *DefaultMitigationCoordinator) triggerScrubbing(ctx context.Context, attack *AttackInfo, mitigation *MitigationInfo) error {
	dmc.logger.Infof("Triggering scrubbing for %s", attack.TargetIP)

	if dmc.scrubbingEndpoint == "" {
		return fmt.Errorf("scrubbing endpoint not configured")
	}

	// Determine if auto-approval is allowed
	approvedBy := ""
	if dmc.config.AutoApprove && attack.Confidence >= dmc.config.AutoApproveThreshold {
		approvedBy = "fastnetmon-auto"
	}

	endpoint := fmt.Sprintf("%s/api/v1/scrubbing/start", dmc.scrubbingEndpoint)
	reqBody := map[string]interface{}{
		"provider":        "arbor-primary",  // или определять динамически
		"prefix":          attack.TargetIP + "/32",
		"protocol":        "all",
		"attack_type":     attack.AttackType,
		"severity":        attack.Severity,
		"duration":        int(dmc.config.DefaultDuration.Seconds()),
		"confidence_score": attack.Confidence,
		"approved_by":     approvedBy,
		"metadata": map[string]interface{}{
			"source":       "fastnetmon",
			"attack_id":    attack.AttackID,
			"mitigation_id": mitigation.ID,
			"peak_traffic": attack.PeakTraffic,
			"peak_packets": attack.PeakPackets,
		},
	}

	respData, err := dmc.makeAPIRequestWithResponse(ctx, "POST", endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("scrubbing API call failed: %w", err)
	}

	// Extract scrub_id from response
	if scrubID, ok := respData["id"].(string); ok {
		mitigation.Metadata["scrub_id"] = scrubID
	}

	mitigation.Metadata["scrubbing_started"] = true
	dmc.logger.Infof("Scrubbing started for %s", attack.TargetIP)
	return nil
}

// makeAPIRequest makes an API request without expecting response data
func (dmc *DefaultMitigationCoordinator) makeAPIRequest(ctx context.Context, method, endpoint string, body interface{}) error {
	_, err := dmc.makeAPIRequestWithResponse(ctx, method, endpoint, body)
	return err
}

// makeAPIRequestWithResponse makes an API request and returns response data
func (dmc *DefaultMitigationCoordinator) makeAPIRequestWithResponse(ctx context.Context, method, endpoint string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := dmc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return respData, nil
}
