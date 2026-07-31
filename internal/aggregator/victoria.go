package aggregator

import "fmt"

func buildVictoriaPayload(
	testID string,
	requests int64,
	successes int64,
	failures int64,
	workers int,
	avgLatency float64,
	maxLatency float64,
	bytesSent int64,
	bytesReceived int64,
) string {

	return fmt.Sprintf(
`vulcan_requests_total{test_id="%s"} %d
vulcan_success_total{test_id="%s"} %d
vulcan_failure_total{test_id="%s"} %d
vulcan_workers{test_id="%s"} %d
vulcan_avg_latency_ms{test_id="%s"} %.2f
vulcan_max_latency_ms{test_id="%s"} %.2f
vulcan_bytes_sent_total{test_id="%s"} %d
vulcan_bytes_received_total{test_id="%s"} %d
`,
		testID,
		requests,
		testID,
		successes,
		testID,
		failures,
		testID,
		workers,
		testID,
		avgLatency,
		testID,
		maxLatency,
		testID,
		bytesSent,
		testID,
		bytesReceived,
	)
}