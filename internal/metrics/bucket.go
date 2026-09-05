package metrics

import "time"

type MetricBucket struct {
	TestID   string
	WorkerID string

	WindowStart time.Time
	WindowEnd   time.Time

	Requests  int64
	Successes int64
	Failures  int64

	BytesSent     int64
	BytesReceived int64

	AvgLatencyMs float64
	MaxLatencyMs float64
}
