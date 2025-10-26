/*
CloudBridge Monitoring - FastNetMon Integration
Main entry point for FastNetMon integration service
*/

package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// FastNetMon API configuration
	fastnetmonEndpoint = flag.String("fastnetmon-endpoint", "http://localhost:10007", "FastNetMon API endpoint")
	fastnetmonAPIKey   = flag.String("fastnetmon-api-key", "", "FastNetMon API key")
	fastnetmonAPISecret = flag.String("fastnetmon-api-secret", "", "FastNetMon API secret")

	// Webhook server configuration
	webhookAddr = flag.String("webhook-addr", ":8080", "Webhook server listen address")

	// Integration endpoints
	scrubbingEndpoint = flag.String("scrubbing-endpoint", "http://scrubbing-manager:8080", "Scrubbing manager endpoint")
	bgpEndpoint       = flag.String("bgp-endpoint", "http://bgp-manager:8080", "BGP manager endpoint")

	// Mitigation configuration
	autoSelectStrategy  = flag.Bool("auto-select-strategy", true, "Automatically select mitigation strategy")
	autoApprove         = flag.Bool("auto-approve", false, "Auto-approve mitigations")
	autoApproveThreshold = flag.Float64("auto-approve-threshold", 0.95, "Auto-approve threshold")
	minConfidence       = flag.Float64("min-confidence", 0.85, "Minimum confidence to trigger mitigation")
	rtbhThresholdGbps   = flag.Float64("rtbh-threshold-gbps", 5.0, "RTBH threshold in Gbps")
	scrubbingThresholdGbps = flag.Float64("scrubbing-threshold-gbps", 10.0, "Scrubbing threshold in Gbps")
	defaultDuration     = flag.Duration("default-duration", 1*time.Hour, "Default mitigation duration")

	// Logging
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Setup logger
	logger := logrus.New()
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logger.Warnf("Invalid log level %s, using info", *logLevel)
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)
	logger.SetFormatter(&logrus.JSONFormatter{})

	logger.Info("Starting CloudBridge FastNetMon Integration...")

	// Create FastNetMon client
	fmConfig := &FastNetMonConfig{
		APIEndpoint: *fastnetmonEndpoint,
		APIKey:      *fastnetmonAPIKey,
		APISecret:   *fastnetmonAPISecret,
		Timeout:     30 * time.Second,
	}

	fastnetmonClient, err := NewFastNetMonClient(fmConfig)
	if err != nil {
		logger.Fatalf("Failed to create FastNetMon client: %v", err)
	}

	// Create mitigation coordinator
	coordinator := NewDefaultMitigationCoordinator(*scrubbingEndpoint, *bgpEndpoint)
	coordinator.SetConfig(&CoordinatorConfig{
		AutoSelectStrategy:    *autoSelectStrategy,
		RTBHThresholdBps:      uint64(*rtbhThresholdGbps * 1_000_000_000),
		ScrubbingThresholdBps: uint64(*scrubbingThresholdGbps * 1_000_000_000),
		MinConfidence:         *minConfidence,
		AutoApprove:           *autoApprove,
		AutoApproveThreshold:  *autoApproveThreshold,
		DefaultDuration:       *defaultDuration,
	})

	logger.Info("Mitigation coordinator configured")
	logger.Infof("  Auto-select strategy: %v", *autoSelectStrategy)
	logger.Infof("  Auto-approve: %v (threshold: %.2f)", *autoApprove, *autoApproveThreshold)
	logger.Infof("  Min confidence: %.2f", *minConfidence)
	logger.Infof("  RTBH threshold: %.1f Gbps", *rtbhThresholdGbps)
	logger.Infof("  Scrubbing threshold: %.1f Gbps", *scrubbingThresholdGbps)

	// Create notification handler
	handler := NewNotificationHandler(coordinator)

	// Set attack handlers on FastNetMon client
	fastnetmonClient.SetAttackHandlers(
		// onAttackStart
		func(attack *AttackInfo) error {
			logger.Infof("Attack started callback: %s targeting %s", attack.AttackID, attack.TargetIP)

			// Trigger mitigation через coordinator
			ctx := context.Background()
			mitigation, err := coordinator.TriggerMitigation(ctx, attack)
			if err != nil {
				logger.Errorf("Failed to trigger mitigation: %v", err)
				return err
			}

			logger.Infof("Mitigation triggered: %s (strategy: %s)", mitigation.ID, mitigation.Strategy)
			return nil
		},
		// onAttackStop
		func(attack *AttackInfo) error {
			logger.Infof("Attack stopped callback: %s targeting %s", attack.AttackID, attack.TargetIP)

			// Stop mitigation
			ctx := context.Background()
			mitigationID := fmt.Sprintf("mitigation-%s", attack.AttackID)
			if err := coordinator.StopMitigation(ctx, mitigationID); err != nil {
				logger.Errorf("Failed to stop mitigation: %v", err)
				return err
			}

			logger.Infof("Mitigation stopped: %s", mitigationID)
			return nil
		},
	)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start FastNetMon client
	if err := fastnetmonClient.Start(ctx); err != nil {
		logger.Fatalf("Failed to start FastNetMon client: %v", err)
	}

	// Start webhook handler in goroutine
	go func() {
		logger.Infof("Starting webhook handler on %s", *webhookAddr)
		if err := handler.StartWithContext(ctx, *webhookAddr); err != nil {
			logger.Errorf("Webhook handler error: %v", err)
		}
	}()

	// Print startup info
	printStartupInfo(logger)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("FastNetMon integration started successfully")
	logger.Info("Press Ctrl+C to stop...")

	<-sigChan
	logger.Info("Shutting down...")

	// Cancel context to stop all goroutines
	cancel()

	// Give time for graceful shutdown
	time.Sleep(2 * time.Second)

	// Stop FastNetMon client
	if err := fastnetmonClient.Stop(); err != nil {
		logger.Errorf("Error stopping FastNetMon client: %v", err)
	}

	logger.Info("FastNetMon integration stopped")
}

// printStartupInfo prints startup information
func printStartupInfo(logger *logrus.Logger) {
	logger.Info("=== CloudBridge FastNetMon Integration ===")
	logger.Infof("FastNetMon API: %s", *fastnetmonEndpoint)
	logger.Infof("Webhook server: %s", *webhookAddr)
	logger.Infof("Scrubbing endpoint: %s", *scrubbingEndpoint)
	logger.Infof("BGP endpoint: %s", *bgpEndpoint)
	logger.Info("")
	logger.Info("Webhook endpoints:")
	logger.Infof("  POST %s/webhook/attack", *webhookAddr)
	logger.Infof("  POST %s/webhook/attack/started", *webhookAddr)
	logger.Infof("  POST %s/webhook/attack/stopped", *webhookAddr)
	logger.Info("")
	logger.Info("API endpoints:")
	logger.Infof("  GET  %s/api/mitigations", *webhookAddr)
	logger.Infof("  GET  %s/api/mitigations/{id}", *webhookAddr)
	logger.Infof("  POST %s/api/mitigations/{id}/stop", *webhookAddr)
	logger.Info("")
	logger.Info("Monitoring:")
	logger.Infof("  GET  %s/health", *webhookAddr)
	logger.Infof("  GET  %s/metrics", *webhookAddr)
	logger.Info("==========================================")
}
