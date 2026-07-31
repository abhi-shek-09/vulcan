package metrics

import (
	"sync"
	"time"
)

type Collector struct {
	mu sync.Mutex
	testID   string
	workerID string
	windowStart time.Time
	requests  int64
	successes int64
	failures  int64
	bytesSent     int64
	bytesReceived int64
	totalLatency time.Duration
	maxLatency   time.Duration
}

func NewCollector(testID, workerID string) *Collector {
	return &Collector{
		testID:      testID,
		workerID:    workerID,
		windowStart: time.Now(),
	}
}

func (c *Collector) Record(result RequestResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.requests++

	if result.Success {
		c.successes++
	} else {
		c.failures++
	}

	c.bytesSent += result.BytesSent
	c.bytesReceived += result.BytesReceived
	c.totalLatency += result.Latency

	if result.Latency > c.maxLatency {
		c.maxLatency = result.Latency
	}
}

func (c *Collector) Flush() MetricBucket {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var avgLatency float64

	if c.requests > 0 {
		avgLatency = float64(c.totalLatency.Milliseconds()) / float64(c.requests)
	}

	bucket := MetricBucket{
		TestID:        c.testID,
		WorkerID:      c.workerID,
		WindowStart:   c.windowStart,
		WindowEnd:     now,
		Requests:      c.requests,
		Successes:     c.successes,
		Failures:      c.failures,
		BytesSent:     c.bytesSent,
		BytesReceived: c.bytesReceived,
		AvgLatencyMs:  avgLatency,
		MaxLatencyMs:  float64(c.maxLatency.Milliseconds()),
	}

	c.windowStart = now
	c.requests = 0
	c.successes = 0
	c.failures = 0
	c.bytesSent = 0
	c.bytesReceived = 0
	c.totalLatency = 0
	c.maxLatency = 0

	return bucket
}