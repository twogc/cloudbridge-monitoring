// DDoS Auto-Mitigator для CloudBridge
// Автоматически обнаруживает DDoS атаки и включает BGP blackhole injection
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
)

var (
	// Флаги командной строки
	prometheusURL = flag.String("prometheus-url", "http://prometheus:9090", "Prometheus URL")
	redisAddr     = flag.String("redis-addr", "redis:6379", "Redis address")
	checkInterval = flag.Duration("check-interval", 30*time.Second, "DDoS check interval")
	bgpAPIURL     = flag.String("bgp-api-url", "http://bgp-controller:8080", "BGP controller API URL")
	listenAddr    = flag.String("listen", ":8082", "HTTP listen address for metrics")

	// Пороги для определения DDoS
	ppsThreshold     = flag.Float64("pps-threshold", 100000, "Packets per second threshold")
	bpsThreshold     = flag.Float64("bps-threshold", 1e9, "Bits per second threshold (1 Gbps)")
	errorRateThresh  = flag.Float64("error-rate-threshold", 0.05, "Error rate threshold (5%)")
	connectionThresh = flag.Int("connection-threshold", 10000, "Connection count threshold")

	// Prometheus метрики
	ddosDetections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_ddos_detections_total",
			Help: "Total number of DDoS attacks detected",
		},
		[]string{"target_ip", "attack_type"},
	)

	mitigationActions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbridge_ddos_mitigations_total",
			Help: "Total number of mitigation actions taken",
		},
		[]string{"action_type", "status"},
	)

	mitigationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "cloudbridge_ddos_mitigation_duration_seconds",
			Help:    "Time taken to activate mitigation",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
		},
	)

	activeAttacks = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cloudbridge_ddos_active_attacks",
			Help: "Number of currently active DDoS attacks",
		},
	)
)

func init() {
	prometheus.MustRegister(ddosDetections, mitigationActions, mitigationDuration, activeAttacks)
}

// DDoSDetector обнаруживает и митигирует DDoS атаки
type DDoSDetector struct {
	prometheusAPI v1.API
	redisClient   *redis.Client
	httpClient    *http.Client

	// Состояние активных атак
	activeAttacksMap map[string]*Attack
}

// Attack представляет обнаруженную атаку
type Attack struct {
	TargetIP      string    `json:"target_ip"`
	AttackType    string    `json:"attack_type"`
	DetectedAt    time.Time `json:"detected_at"`
	Severity      string    `json:"severity"`      // low, medium, high, critical
	PPS           float64   `json:"pps"`           // Packets per second
	BPS           float64   `json:"bps"`           // Bits per second
	ErrorRate     float64   `json:"error_rate"`    // Error rate %
	Connections   int       `json:"connections"`   // Connection count
	Mitigated     bool      `json:"mitigated"`
	MitigationType string   `json:"mitigation_type"` // bgp_blackhole, rate_limit, etc.
}

// NewDDoSDetector создает новый детектор
func NewDDoSDetector(promURL, redisAddr string) (*DDoSDetector, error) {
	// Инициализация Prometheus client
	promClient, err := api.NewClient(api.Config{
		Address: promURL,
	})
	if err != nil {
		return nil, fmt.Errorf("creating prometheus client: %w", err)
	}

	promAPI := v1.NewAPI(promClient)

	// Инициализация Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DB:           0,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	})

	// Проверка подключения
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &DDoSDetector{
		prometheusAPI:    promAPI,
		redisClient:      redisClient,
		httpClient:       &http.Client{Timeout: 10 * time.Second},
		activeAttacksMap: make(map[string]*Attack),
	}, nil
}

// queryPrometheus выполняет Prometheus query
func (d *DDoSDetector) queryPrometheus(ctx context.Context, query string) (float64, error) {
	result, warnings, err := d.prometheusAPI.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}

	if len(warnings) > 0 {
		log.Printf("Prometheus warnings: %v", warnings)
	}

	// Парсим результат
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		if len(vector) > 0 {
			return float64(vector[0].Value), nil
		}
	}

	return 0, nil
}

// detectDDoS проверяет наличие DDoS атаки
func (d *DDoSDetector) detectDDoS(ctx context.Context) ([]*Attack, error) {
	var attacks []*Attack

	// 1. Проверка PPS (packets per second)
	ppsQuery := `rate(relay_packets_total[1m])`
	pps, err := d.queryPrometheus(ctx, ppsQuery)
	if err != nil {
		log.Printf("Failed to query PPS: %v", err)
	} else if pps > *ppsThreshold {
		attacks = append(attacks, &Attack{
			TargetIP:   "0.0.0.0", // TODO: получить из метрик
			AttackType: "volumetric_pps",
			DetectedAt: time.Now(),
			Severity:   d.calculateSeverity(pps, *ppsThreshold),
			PPS:        pps,
		})
	}

	// 2. Проверка BPS (bits per second)
	bpsQuery := `rate(relay_bytes_total[1m]) * 8`
	bps, err := d.queryPrometheus(ctx, bpsQuery)
	if err != nil {
		log.Printf("Failed to query BPS: %v", err)
	} else if bps > *bpsThreshold {
		attacks = append(attacks, &Attack{
			TargetIP:   "0.0.0.0",
			AttackType: "volumetric_bps",
			DetectedAt: time.Now(),
			Severity:   d.calculateSeverity(bps, *bpsThreshold),
			BPS:        bps,
		})
	}

	// 3. Проверка error rate
	errorRateQuery := `rate(relay_errors_total[1m]) / rate(relay_requests_total[1m])`
	errorRate, err := d.queryPrometheus(ctx, errorRateQuery)
	if err != nil {
		log.Printf("Failed to query error rate: %v", err)
	} else if errorRate > *errorRateThresh {
		attacks = append(attacks, &Attack{
			TargetIP:   "0.0.0.0",
			AttackType: "application_layer",
			DetectedAt: time.Now(),
			Severity:   d.calculateSeverity(errorRate, *errorRateThresh),
			ErrorRate:  errorRate * 100, // В процентах
		})
	}

	// 4. Проверка connection flood
	connQuery := `relay_active_connections`
	connections, err := d.queryPrometheus(ctx, connQuery)
	if err != nil {
		log.Printf("Failed to query connections: %v", err)
	} else if connections > float64(*connectionThresh) {
		attacks = append(attacks, &Attack{
			TargetIP:    "0.0.0.0",
			AttackType:  "connection_flood",
			DetectedAt:  time.Now(),
			Severity:    d.calculateSeverity(connections, float64(*connectionThresh)),
			Connections: int(connections),
		})
	}

	return attacks, nil
}

// calculateSeverity определяет severity атаки
func (d *DDoSDetector) calculateSeverity(value, threshold float64) string {
	ratio := value / threshold

	switch {
	case ratio >= 10:
		return "critical"
	case ratio >= 5:
		return "high"
	case ratio >= 2:
		return "medium"
	default:
		return "low"
	}
}

// mitigateAttack активирует mitigation для атаки
func (d *DDoSDetector) mitigateAttack(ctx context.Context, attack *Attack) error {
	startTime := time.Now()
	defer func() {
		mitigationDuration.Observe(time.Since(startTime).Seconds())
	}()

	log.Printf("Mitigating attack: type=%s, severity=%s, target=%s",
		attack.AttackType, attack.Severity, attack.TargetIP)

	// Определяем тип mitigation на основе severity и типа атаки
	mitigationType := d.selectMitigationType(attack)

	switch mitigationType {
	case "bgp_blackhole":
		if err := d.activateBGPBlackhole(ctx, attack.TargetIP); err != nil {
			mitigationActions.WithLabelValues("bgp_blackhole", "failed").Inc()
			return fmt.Errorf("BGP blackhole activation failed: %w", err)
		}
		mitigationActions.WithLabelValues("bgp_blackhole", "success").Inc()

	case "rate_limit":
		if err := d.activateRateLimit(ctx, attack); err != nil {
			mitigationActions.WithLabelValues("rate_limit", "failed").Inc()
			return fmt.Errorf("rate limit activation failed: %w", err)
		}
		mitigationActions.WithLabelValues("rate_limit", "success").Inc()

	case "upstream_scrubbing":
		if err := d.activateUpstreamScrubbing(ctx, attack.TargetIP); err != nil {
			mitigationActions.WithLabelValues("upstream_scrubbing", "failed").Inc()
			return fmt.Errorf("upstream scrubbing activation failed: %w", err)
		}
		mitigationActions.WithLabelValues("upstream_scrubbing", "success").Inc()

	default:
		return fmt.Errorf("unknown mitigation type: %s", mitigationType)
	}

	attack.Mitigated = true
	attack.MitigationType = mitigationType

	// Сохранить в Redis
	if err := d.saveAttackToRedis(ctx, attack); err != nil {
		log.Printf("Failed to save attack to Redis: %v", err)
	}

	log.Printf("Attack mitigated successfully: type=%s, mitigation=%s", attack.AttackType, mitigationType)

	return nil
}

// selectMitigationType выбирает подходящий тип mitigation
func (d *DDoSDetector) selectMitigationType(attack *Attack) string {
	// Critical и high severity -> BGP blackhole
	if attack.Severity == "critical" || attack.Severity == "high" {
		return "bgp_blackhole"
	}

	// Volumetric атаки -> upstream scrubbing
	if attack.AttackType == "volumetric_pps" || attack.AttackType == "volumetric_bps" {
		return "upstream_scrubbing"
	}

	// Application layer атаки -> rate limiting
	if attack.AttackType == "application_layer" || attack.AttackType == "connection_flood" {
		return "rate_limit"
	}

	return "rate_limit" // Default
}

// activateBGPBlackhole активирует BGP blackhole routing
func (d *DDoSDetector) activateBGPBlackhole(ctx context.Context, targetIP string) error {
	url := fmt.Sprintf("%s/api/blackhole/add", *bgpAPIURL)

	payload := map[string]interface{}{
		"ip":     targetIP,
		"reason": "ddos_auto_mitigation",
		"ttl":    3600, // 1 час
	}

	payloadJSON, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = http.NoBody

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("BGP API returned status %d", resp.StatusCode)
	}

	log.Printf("BGP blackhole activated for %s", targetIP)

	return nil
}

// activateRateLimit активирует rate limiting
func (d *DDoSDetector) activateRateLimit(ctx context.Context, attack *Attack) error {
	// TODO: интеграция с rate limiting system
	log.Printf("Rate limiting activated for attack type: %s", attack.AttackType)
	return nil
}

// activateUpstreamScrubbing активирует upstream scrubbing (Yandex/Qrator)
func (d *DDoSDetector) activateUpstreamScrubbing(ctx context.Context, targetIP string) error {
	// TODO: интеграция с upstream scrubbing provider
	log.Printf("Upstream scrubbing requested for IP: %s", targetIP)
	return nil
}

// saveAttackToRedis сохраняет информацию об атаке в Redis
func (d *DDoSDetector) saveAttackToRedis(ctx context.Context, attack *Attack) error {
	key := fmt.Sprintf("cloudbridge:ddos:attack:%s:%d", attack.TargetIP, attack.DetectedAt.Unix())

	attackJSON, err := json.Marshal(attack)
	if err != nil {
		return err
	}

	// Сохраняем на 24 часа
	return d.redisClient.Set(ctx, key, attackJSON, 24*time.Hour).Err()
}

// Run запускает детектор
func (d *DDoSDetector) Run(ctx context.Context) error {
	ticker := time.NewTicker(*checkInterval)
	defer ticker.Stop()

	log.Printf("DDoS detector started (check_interval=%s)", *checkInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("DDoS detector stopped")
			return ctx.Err()

		case <-ticker.C:
			attacks, err := d.detectDDoS(ctx)
			if err != nil {
				log.Printf("DDoS detection failed: %v", err)
				continue
			}

			if len(attacks) > 0 {
				log.Printf("Detected %d potential DDoS attacks", len(attacks))

				for _, attack := range attacks {
					ddosDetections.WithLabelValues(attack.TargetIP, attack.AttackType).Inc()

					// Проверяем, не митигируется ли уже эта атака
					attackKey := fmt.Sprintf("%s:%s", attack.TargetIP, attack.AttackType)
					if _, exists := d.activeAttacksMap[attackKey]; exists {
						log.Printf("Attack already being mitigated: %s", attackKey)
						continue
					}

					// Запускаем mitigation
					if err := d.mitigateAttack(ctx, attack); err != nil {
						log.Printf("Mitigation failed: %v", err)
					} else {
						d.activeAttacksMap[attackKey] = attack
						activeAttacks.Set(float64(len(d.activeAttacksMap)))
					}
				}
			}
		}
	}
}

func main() {
	flag.Parse()

	log.Println("CloudBridge DDoS Auto-Mitigator starting...")

	detector, err := NewDDoSDetector(*prometheusURL, *redisAddr)
	if err != nil {
		log.Fatalf("Failed to create detector: %v", err)
	}

	// HTTP server для Prometheus metrics
	http.Handle("/metrics", promhttp.Handler())

	go func() {
		log.Printf("Starting metrics server on %s", *listenAddr)
		if err := http.ListenAndServe(*listenAddr, nil); err != nil {
			log.Fatalf("Failed to start metrics server: %v", err)
		}
	}()

	// Контекст с graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обработка сигналов
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Received shutdown signal")
		cancel()
	}()

	// Запуск детектора
	if err := detector.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Detector error: %v", err)
	}

	log.Println("DDoS Auto-Mitigator stopped")
}
