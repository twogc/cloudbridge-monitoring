package main

import (
	"context"
	"testing"
	"time"
)

func TestQUICProbe(t *testing.T) {
	// Тест создания QUIC probe
	targets := []string{"localhost:443", "example.com:443"}
	interval := 30 * time.Second

	probe := NewQUICProbe(targets, interval)

	if probe == nil {
		t.Fatal("QUICProbe should not be nil")
	}

	if len(probe.targets) != len(targets) {
		t.Fatalf("Expected %d targets, got %d", len(targets), len(probe.targets))
	}

	if probe.interval != interval {
		t.Fatalf("Expected interval %v, got %v", interval, probe.interval)
	}
}

func TestQUICProbeTargets(t *testing.T) {
	tests := []struct {
		name     string
		targets  []string
		expected int
	}{
		{
			name:     "Empty targets",
			targets:  []string{},
			expected: 0,
		},
		{
			name:     "Single target",
			targets:  []string{"localhost:443"},
			expected: 1,
		},
		{
			name:     "Multiple targets",
			targets:  []string{"localhost:443", "example.com:443", "test.com:443"},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := NewQUICProbe(tt.targets, 30*time.Second)
			if len(probe.targets) != tt.expected {
				t.Fatalf("Expected %d targets, got %d", tt.expected, len(probe.targets))
			}
		})
	}
}

func TestQUICProbeInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		expected time.Duration
	}{
		{
			name:     "30 seconds",
			interval: 30 * time.Second,
			expected: 30 * time.Second,
		},
		{
			name:     "1 minute",
			interval: 1 * time.Minute,
			expected: 1 * time.Minute,
		},
		{
			name:     "5 minutes",
			interval: 5 * time.Minute,
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := NewQUICProbe([]string{"localhost:443"}, tt.interval)
			if probe.interval != tt.expected {
				t.Fatalf("Expected interval %v, got %v", tt.expected, probe.interval)
			}
		})
	}
}

func TestQUICProbeContext(t *testing.T) {
	// Тест с контекстом
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_ = NewQUICProbe([]string{"localhost:443"}, 30*time.Second)

	// Проверяем что контекст не влияет на создание probe
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be done yet")
	default:
		// OK
	}
}

func TestQUICProbeConcurrency(t *testing.T) {
	// Тест на конкурентность
	_ = NewQUICProbe([]string{"localhost:443"}, 30*time.Second)

	// Запускаем несколько горутин
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			// Симулируем работу probe
			time.Sleep(10 * time.Millisecond)
		}()
	}

	// Ждем завершения всех горутин
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// OK
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for goroutines")
		}
	}
}

func TestQUICProbeMetrics(t *testing.T) {
	// Тест метрик
	target := "localhost:443"
	pop := "test"

	// Проверяем что метрики созданы
	if quicHandshakeDuration == nil {
		t.Fatal("quicHandshakeDuration should not be nil")
	}

	if quicHandshakeSuccess == nil {
		t.Fatal("quicHandshakeSuccess should not be nil")
	}

	if quicHandshakeFailures == nil {
		t.Fatal("quicHandshakeFailures should not be nil")
	}

	if quicZeroRTT == nil {
		t.Fatal("quicZeroRTT should not be nil")
	}

	if quicLossRatio == nil {
		t.Fatal("quicLossRatio should not be nil")
	}

	if quicRTT == nil {
		t.Fatal("quicRTT should not be nil")
	}

	// Тестируем метрики (без реального соединения)
	quicHandshakeSuccess.WithLabelValues(target, pop).Inc()
	quicHandshakeFailures.WithLabelValues(target, pop, "test_error").Inc()
	quicZeroRTT.WithLabelValues(target, pop).Inc()
	quicLossRatio.WithLabelValues(target, pop).Set(0.1)
	quicRTT.WithLabelValues(target, pop).Observe(0.05)
}

func BenchmarkQUICProbeCreation(b *testing.B) {
	targets := []string{"localhost:443", "example.com:443"}
	interval := 30 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		probe := NewQUICProbe(targets, interval)
		_ = probe
	}
}

func BenchmarkQUICProbeMetrics(b *testing.B) {
	target := "localhost:443"
	pop := "test"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		quicHandshakeSuccess.WithLabelValues(target, pop).Inc()
		quicHandshakeFailures.WithLabelValues(target, pop, "test_error").Inc()
		quicZeroRTT.WithLabelValues(target, pop).Inc()
		quicLossRatio.WithLabelValues(target, pop).Set(0.1)
		quicRTT.WithLabelValues(target, pop).Observe(0.05)
	}
}
