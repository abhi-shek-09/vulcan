package aggregator

import (
	"context"
	"github.com/nats-io/nats.go"
	"log/slog"
	"time"
	"vulcan/internal/config"
	"vulcan/internal/victoria"
)

type Aggregator struct {
	logger *slog.Logger
	config *config.Config
	conn   *nats.Conn
	store  *Store
	writer *victoria.Writer
}

func New(cfg *config.Config, logger *slog.Logger) (*Aggregator, error) {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return nil, err
	}

	return &Aggregator{
		logger: logger,
		config: cfg,
		conn:   nc,
		store:  NewStore(),
		writer: victoria.New(cfg.VictoriaMetricsURL),
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

	snapshots := make([]Snapshot, 0, len(a.store.tests))

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

		successRate := 0.0
		if window.Requests > 0 {
			successRate = (float64(window.Successes) / float64(window.Requests)) * 100
		}

		snapshots = append(snapshots, Snapshot{
			TestID:        window.TestID,
			Workers:       len(window.Workers),
			Requests:      window.Requests,
			Successes:     window.Successes,
			Failures:      window.Failures,
			BytesSent:     window.BytesSent,
			BytesReceived: window.BytesReceived,
			AvgLatency:    avgLatency,
			MaxLatency:    window.MaxLatency,
			SuccessRate:   successRate,
			RPS:           window.Requests,
		})

		window.Requests = 0
		window.Successes = 0
		window.Failures = 0
		window.BytesSent = 0
		window.BytesReceived = 0
		window.TotalLatency = 0
		window.MaxLatency = 0
		window.WindowStart = time.Time{}
		window.WindowEnd = time.Time{}
		window.Workers = make(map[string]struct{})

		window.mu.Unlock()
	}

	a.store.mu.Unlock()

	// Everything below runs WITHOUT any locks.

	for _, s := range snapshots {

		a.logger.Info(
			"global metrics",
			"test_id", s.TestID,
			"workers", s.Workers,
			"rps", s.RPS,
			"requests", s.Requests,
			"successes", s.Successes,
			"failures", s.Failures,
			"success_rate", s.SuccessRate,
			"avg_latency_ms", s.AvgLatency,
			"max_latency_ms", s.MaxLatency,
			"bytes_sent", s.BytesSent,
			"bytes_received", s.BytesReceived,
		)

		payload := buildVictoriaPayload(
			s.TestID,

			s.Requests,
			s.Successes,
			s.Failures,

			s.Workers,

			s.AvgLatency,
			s.MaxLatency,

			s.BytesSent,
			s.BytesReceived,
		)

		if err := a.writer.Write(
			context.Background(),
			payload,
		); err != nil {

			a.logger.Error(
				"victoria write failed",
				"error", err,
			)
		}
	}
}
