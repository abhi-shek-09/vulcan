package execution

import "time"

type Result struct {
	StatusCode    int
	Latency       time.Duration
	Success       bool
	BytesSent     int64
	BytesReceived int64
	Err           error
}
