package dashboard

import "fmt"

func metricQuery(
	metric string,
	testID string,
) string {
	return fmt.Sprintf(
		`%s{test_id="%s"}`,
		metric,
		testID,
	)
}

func RequestsQuery(testID string) string {
	return metricQuery(
		"vulcan_requests_total",
		testID,
	)
}

func SuccessQuery(testID string) string {
	return metricQuery(
		"vulcan_success_total",
		testID,
	)
}

func FailureQuery(testID string) string {
	return metricQuery(
		"vulcan_failure_total",
		testID,
	)
}

func WorkersQuery(testID string) string {
	return metricQuery(
		"vulcan_workers",
		testID,
	)
}

func AvgLatencyQuery(testID string) string {
	return metricQuery(
		"vulcan_avg_latency_ms",
		testID,
	)
}

func MaxLatencyQuery(testID string) string {
	return metricQuery(
		"vulcan_max_latency_ms",
		testID,
	)
}

func BytesSentQuery(testID string) string {
	return metricQuery(
		"vulcan_bytes_sent_total",
		testID,
	)
}

func BytesReceivedQuery(testID string) string {
	return metricQuery(
		"vulcan_bytes_received_total",
		testID,
	)
}
