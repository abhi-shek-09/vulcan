package aggregator

import (
	"sync"
	"time"

	"vulcan/internal/metrics"
)

type GlobalWindow struct {
	mu            sync.Mutex
	TestID        string
	WindowStart   time.Time
	WindowEnd     time.Time
	Requests      int64
	Successes     int64
	Failures      int64
	BytesSent     int64
	BytesReceived int64
	TotalLatency  float64
	MaxLatency    float64
	Workers       map[string]struct{}
}

func NewGlobalWindow(testID string) *GlobalWindow {
	return &GlobalWindow{
		TestID:  testID,
		Workers: make(map[string]struct{}),
	}
}

func (g *GlobalWindow) Merge(bucket metrics.MetricBucket) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.WindowStart.IsZero() {
		g.WindowStart = bucket.WindowStart
	}

	g.WindowEnd = bucket.WindowEnd
	g.Requests += bucket.Requests
	g.Successes += bucket.Successes
	g.Failures += bucket.Failures
	g.BytesSent += bucket.BytesSent
	g.BytesReceived += bucket.BytesReceived
	g.TotalLatency += bucket.AvgLatencyMs * float64(bucket.Requests)

	if bucket.MaxLatencyMs > g.MaxLatency {
		g.MaxLatency = bucket.MaxLatencyMs
	}

	g.Workers[bucket.WorkerID] = struct{}{}
}
