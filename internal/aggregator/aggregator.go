package aggregator

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

type Aggregator struct {
	logger *slog.Logger
	config *Config
	conn *nats.Conn
	store *Store
}

func New(cfg *Config, logger *slog.Logger) (*Aggregator, error) {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return nil, err
	}

	return &Aggregator{
		logger: logger,
		config: cfg,
		conn:   nc,
		store: NewStore(),
	}, nil
}

func (a *Aggregator) Close() {
	a.conn.Close()
}

func (a *Aggregator) Run(ctx context.Context) error {
	defer a.Close()
	go a.reportLoop(ctx)
	return a.subscribe(ctx)
}

func (a *Aggregator) report() {

	a.store.mu.Lock()
	defer a.store.mu.Unlock()

	for testID, window := range a.store.tests {

		window.mu.Lock()
		if window.Requests == 0 {
			window.mu.Unlock()
			delete(a.store.tests, testID)
			continue
		}

		avgLatency := 0.0
		if window.Requests > 0 {
			avgLatency = window.TotalLatency / float64(window.Requests)
		}

		rps := window.Requests

		successRate := 0.0

		if window.Requests > 0 {
			successRate = (float64(window.Successes) / float64(window.Requests)) * 100
		}

		a.logger.Info(
			"global metrics",
			"test_id", window.TestID,
			"workers", len(window.Workers),
			"rps", rps,
			"requests", window.Requests,
			"successes", window.Successes,
			"failures", window.Failures,
			"success_rate", successRate,
			"avg_latency_ms", avgLatency,
			"max_latency_ms", window.MaxLatency,
			"bytes_sent", window.BytesSent,
			"bytes_received", window.BytesReceived,
		)

		window.Requests = 0
		window.Successes = 0
		window.Failures = 0
		window.BytesSent = 0
		window.BytesReceived = 0
		window.TotalLatency = 0
		window.MaxLatency = 0
		window.Workers = make(map[string]struct{})
		window.WindowStart = time.Time{}
		window.WindowEnd = time.Time{}

		window.mu.Unlock()
	}
}