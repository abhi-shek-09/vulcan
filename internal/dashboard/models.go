package dashboard

type LiveMetrics struct {
	TestID string `json:"test_id"`
	Requests int64 `json:"requests"`
	Successes int64 `json:"successes"`
	Failures int64 `json:"failures"`
	Workers int64 `json:"workers"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
	BytesSent int64 `json:"bytes_sent"`
	BytesReceived int64 `json:"bytes_received"`
}


type DataPoint struct {
	Timestamp int64
	Value float64
}

type MetricSeries struct {
	Metric string      `json:"metric"`
	Points []DataPoint `json:"points"`
}


type HistoryMetrics struct {
	Series []MetricSeries `json:"series"`
}