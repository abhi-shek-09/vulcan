package aggregator

type Snapshot struct {
	TestID string
	Workers int
	Requests int64
	Successes int64
	Failures int64
	BytesSent int64
	BytesReceived int64
	AvgLatency float64
	MaxLatency float64
	SuccessRate float64
	RPS int64
}