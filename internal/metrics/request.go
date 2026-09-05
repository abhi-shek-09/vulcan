package metrics

import "time"

type RequestResult struct {
	Latency time.Duration

	Success bool

	BytesSent     int64
	BytesReceived int64
}
