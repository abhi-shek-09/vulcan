package aggregator

import (
	"fmt"
	"time"
)

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

	ts := time.Now().UnixMilli()

	return fmt.Sprintf(
		`vulcan_requests_total{test_id="%s"} %d %d
vulcan_success_total{test_id="%s"} %d %d
vulcan_failure_total{test_id="%s"} %d %d
vulcan_workers{test_id="%s"} %d %d
vulcan_avg_latency_ms{test_id="%s"} %.2f %d
vulcan_max_latency_ms{test_id="%s"} %.2f %d
vulcan_bytes_sent_total{test_id="%s"} %d %d
vulcan_bytes_received_total{test_id="%s"} %d %d
`,
		testID, requests, ts,
		testID, successes, ts,
		testID, failures, ts,
		testID, workers, ts,
		testID, avgLatency, ts,
		testID, maxLatency, ts,
		testID, bytesSent, ts,
		testID, bytesReceived, ts,
	)
}
