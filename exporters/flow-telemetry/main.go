/*
CloudBridge Monitoring - Flow Telemetry Collector
Main entry point for flow telemetry collection and export
*/

package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	sflowPort     = flag.Int("sflow-port", 6343, "sFlow collector port")
	netflowPort   = flag.Int("netflow-port", 2055, "NetFlow collector port")
	ipfixPort     = flag.Int("ipfix-port", 4739, "IPFIX collector port")
	metricsPort   = flag.String("metrics-port", ":9090", "Prometheus metrics port")
	windowDur     = flag.Duration("window", 60*time.Second, "Aggregation window duration")
	logLevel      = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	enableSFlow   = flag.Bool("enable-sflow", true, "Enable sFlow collector")
	enableNetFlow = flag.Bool("enable-netflow", true, "Enable NetFlow collector")
	enableIPFIX   = flag.Bool("enable-ipfix", true, "Enable IPFIX collector")
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

	logger.Info("Starting CloudBridge Flow Telemetry Collector...")
	logger.Infof("Configuration: sFlow=%v:%d, NetFlow=%v:%d, IPFIX=%v:%d",
		*enableSFlow, *sflowPort, *enableNetFlow, *netflowPort, *enableIPFIX, *ipfixPort)

	// Create aggregator
	aggregator := NewFlowAggregator(*windowDur)
	logger.Infof("Flow aggregator created with %v window", *windowDur)

	// Determine which ports to enable
	sflowPortNum := 0
	if *enableSFlow {
		sflowPortNum = *sflowPort
	}
	netflowPortNum := 0
	if *enableNetFlow {
		netflowPortNum = *netflowPort
	}
	ipfixPortNum := 0
	if *enableIPFIX {
		ipfixPortNum = *ipfixPort
	}

	// Create collector
	collector, err := NewFlowCollector(sflowPortNum, netflowPortNum, ipfixPortNum, aggregator)
	if err != nil {
		logger.Fatalf("Failed to create flow collector: %v", err)
	}

	// Create exporter
	exporter := NewFlowExporter(aggregator)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	if err := collector.Start(ctx); err != nil {
		logger.Fatalf("Failed to start collector: %v", err)
	}

	// Start exporter in goroutine
	go func() {
		logger.Infof("Starting metrics exporter on %s", *metricsPort)
		if err := exporter.ServeMetrics(*metricsPort); err != nil {
			logger.Errorf("Metrics server error: %v", err)
		}
	}()

	// Print startup info
	printStartupInfo(logger)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("Flow telemetry collector started successfully")
	logger.Info("Press Ctrl+C to stop...")

	<-sigChan
	logger.Info("Shutting down...")

	// Cancel context to stop all goroutines
	cancel()

	// Give time for graceful shutdown
	time.Sleep(2 * time.Second)

	// Stop collector
	if err := collector.Stop(); err != nil {
		logger.Errorf("Error stopping collector: %v", err)
	}

	logger.Info("Flow telemetry collector stopped")
}

// printStartupInfo prints startup information
func printStartupInfo(logger *logrus.Logger) {
	info := map[string]interface{}{
		"service":    "cloudbridge-flow-telemetry",
		"version":    "1.0.0",
		"collectors": map[string]interface{}{},
		"endpoints": map[string]string{
			"metrics":     *metricsPort + "/metrics",
			"health":      *metricsPort + "/health",
			"top_talkers": *metricsPort + "/api/top-talkers",
			"ip_stats":    *metricsPort + "/api/ip-stats?ip=<IP>",
			"anomalies":   *metricsPort + "/api/anomalies",
		},
		"configuration": map[string]interface{}{
			"aggregation_window": windowDur.String(),
			"log_level":          *logLevel,
		},
	}

	collectorsMap := info["collectors"].(map[string]interface{})
	if *enableSFlow {
		collectorsMap["sflow"] = map[string]interface{}{
			"enabled":  true,
			"port":     *sflowPort,
			"protocol": "UDP",
		}
	}
	if *enableNetFlow {
		collectorsMap["netflow"] = map[string]interface{}{
			"enabled":  true,
			"port":     *netflowPort,
			"protocol": "UDP",
			"version":  "v5",
		}
	}
	if *enableIPFIX {
		collectorsMap["ipfix"] = map[string]interface{}{
			"enabled":  true,
			"port":     *ipfixPort,
			"protocol": "UDP",
		}
	}

	infoJSON, _ := json.MarshalIndent(info, "", "  ")
	logger.Infof("Startup info:\n%s", string(infoJSON))
}
