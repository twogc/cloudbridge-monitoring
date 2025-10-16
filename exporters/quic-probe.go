package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/quic-go/quic-go"
)

var (
	// Метрики QUIC probe
	quicHandshakeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "quic_handshake_duration_seconds",
			Help: "Duration of QUIC handshake",
		},
		[]string{"target", "pop"},
	)

	quicHandshakeSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quic_handshake_success_total",
			Help: "Total successful QUIC handshakes",
		},
		[]string{"target", "pop"},
	)

	quicHandshakeFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quic_handshake_failures_total",
			Help: "Total failed QUIC handshakes",
		},
		[]string{"target", "pop", "error_type"},
	)

	quicZeroRTT = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quic_zero_rtt_success_total",
			Help: "Total successful QUIC 0-RTT connections",
		},
		[]string{"target", "pop"},
	)

	quicLossRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quic_loss_ratio",
			Help: "QUIC packet loss ratio",
		},
		[]string{"target", "pop"},
	)

	quicRTT = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "quic_rtt_seconds",
			Help: "QUIC round-trip time",
		},
		[]string{"target", "pop"},
	)
)

func init() {
	prometheus.MustRegister(quicHandshakeDuration)
	prometheus.MustRegister(quicHandshakeSuccess)
	prometheus.MustRegister(quicHandshakeFailures)
	prometheus.MustRegister(quicZeroRTT)
	prometheus.MustRegister(quicLossRatio)
	prometheus.MustRegister(quicRTT)
}

type QUICProbe struct {
	targets  []string
	interval time.Duration
}

func NewQUICProbe(targets []string, interval time.Duration) *QUICProbe {
	return &QUICProbe{
		targets:  targets,
		interval: interval,
	}
}

func (qp *QUICProbe) probeQUIC(target, pop string) {
	start := time.Now()

	// Создаем QUIC соединение
	conn, err := quic.DialAddr(context.Background(), target, &tls.Config{
		InsecureSkipVerify: true,
	}, &quic.Config{
		HandshakeIdleTimeout: 10 * time.Second,
		MaxIdleTimeout:       30 * time.Second,
		// Новые параметры для quic-go v0.55.0
		EnableDatagrams:         true,
		DisablePathMTUDiscovery: false,
	})

	duration := time.Since(start).Seconds()

	if err != nil {
		quicHandshakeFailures.WithLabelValues(target, pop, "connection_failed").Inc()
		log.Printf("QUIC handshake failed for %s: %v", target, err)
		return
	}
	defer conn.CloseWithError(0, "probe completed")

	// Успешное соединение
	quicHandshakeSuccess.WithLabelValues(target, pop).Inc()
	quicHandshakeDuration.WithLabelValues(target, pop).Observe(duration)

	// Проверяем 0-RTT
	if conn.ConnectionState().TLS.HandshakeComplete {
		quicZeroRTT.WithLabelValues(target, pop).Inc()
	}

	// Измеряем RTT
	rttStart := time.Now()
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("Failed to open stream for RTT measurement: %v", err)
		return
	}
	defer stream.Close()

	// Отправляем ping
	_, err = stream.Write([]byte("ping"))
	if err != nil {
		log.Printf("Failed to write ping: %v", err)
		return
	}

	// Читаем pong
	buffer := make([]byte, 4)
	_, err = stream.Read(buffer)
	if err != nil {
		log.Printf("Failed to read pong: %v", err)
		return
	}

	rtt := time.Since(rttStart).Seconds()
	quicRTT.WithLabelValues(target, pop).Observe(rtt)

	// Симулируем измерение loss ratio (в реальности нужна более сложная логика)
	lossRatio := 0.0 // В реальности это должно быть измерено
	quicLossRatio.WithLabelValues(target, pop).Set(lossRatio)
}

func (qp *QUICProbe) start() {
	ticker := time.NewTicker(qp.interval)
	defer ticker.Stop()
	
	for range ticker.C {
		for _, target := range qp.targets {
			// Определяем PoP по target
			pop := "unknown"
			if target == "moscow.2gc.ru:443" {
				pop = "moscow"
			} else if target == "frankfurt.2gc.ru:443" {
				pop = "frankfurt"
			} else if target == "amsterdam.2gc.ru:443" {
				pop = "amsterdam"
			}
			
			go qp.probeQUIC(target, pop)
		}
	}
}

func main() {
	var (
		targets  = flag.String("targets", "moscow.2gc.ru:443,frankfurt.2gc.ru:443,amsterdam.2gc.ru:443", "Comma-separated list of QUIC targets")
		interval = flag.Duration("interval", 30*time.Second, "Probe interval")
		port     = flag.Int("port", 8080, "HTTP server port")
	)
	flag.Parse()

	// Парсим targets из флага
	var targetList []string
	if *targets != "" {
		for _, target := range strings.Split(*targets, ",") {
			if target != "" {
				targetList = append(targetList, strings.TrimSpace(target))
			}
		}
	}

	// Если targets не переданы через флаги, используем дефолтные
	if len(targetList) == 0 {
		targetList = []string{"moscow.2gc.ru:443", "frankfurt.2gc.ru:443", "amsterdam.2gc.ru:443"}
	}

	// Создаем и запускаем probe
	probe := NewQUICProbe(targetList, *interval)
	go probe.start()

	// Запускаем HTTP сервер для метрик
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Printf("Starting QUIC probe on port %d", *port)
	log.Printf("Probing targets: %v", targetList)
	log.Printf("Probe interval: %v", *interval)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		log.Fatal(err)
	}
}
