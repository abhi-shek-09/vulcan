package metrics

import (
	"context"
	"math/rand"
	"time"
)

type Simulator struct {
	collector *Collector
}

func NewSimulator(collector *Collector) *Simulator {
	return &Simulator{
		collector: collector,
	}
}

func (s *Simulator) Run(ctx context.Context) {

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			latency := time.Duration(10+rand.Intn(190)) * time.Millisecond
			success := rand.Intn(100) < 95
			req := RequestResult{
				Latency:      latency,
				Success:      success,
				BytesSent:    int64(500 + rand.Intn(1000)),
				BytesReceived: int64(1000 + rand.Intn(4000)),
			}

			s.collector.Record(req)
		}
	}
}