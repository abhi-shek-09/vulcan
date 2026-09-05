package dashboard

type Service struct {
	client *Client
}

func NewService(
	client *Client,
) *Service {
	return &Service{
		client: client,
	}
}

func (s *Service) GetLiveMetrics(
	testID string,
) (*LiveMetrics, error) {
	requests, err := s.client.Query(
		RequestsQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	successes, err := s.client.Query(
		SuccessQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	failures, err := s.client.Query(
		FailureQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	workers, err := s.client.Query(
		WorkersQuery(testID),
	)
	if err != nil {
		return nil, err
	}

	avgLatency, err := s.client.Query(
		AvgLatencyQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	maxLatency, err := s.client.Query(
		MaxLatencyQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	bytesSent, err := s.client.Query(
		BytesSentQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	bytesReceived, err := s.client.Query(
		BytesReceivedQuery(testID),
	)

	if err != nil {
		return nil, err
	}

	return &LiveMetrics{
		TestID:        testID,
		Requests:      int64(ParseValue(requests)),
		Successes:     int64(ParseValue(successes)),
		Failures:      int64(ParseValue(failures)),
		Workers:       int64(ParseValue(workers)),
		AvgLatencyMs:  ParseValue(avgLatency),
		MaxLatencyMs:  ParseValue(maxLatency),
		BytesSent:     int64(ParseValue(bytesSent)),
		BytesReceived: int64(ParseValue(bytesReceived)),
	}, nil

}

func (s *Service) GetHistory(
	testID string,
	start string,
	end string,
	step string,
) (*HistoryMetrics, error) {
	metrics := []struct {
		name  string
		query string
	}{
		{
			"requests",
			RequestsQuery(testID),
		},
		{
			"latency",
			AvgLatencyQuery(testID),
		},
		{
			"workers",
			WorkersQuery(testID),
		},
	}

	response := HistoryMetrics{
		Series: make([]MetricSeries, 0),
	}

	for _, metric := range metrics {
		result, err := s.client.QueryRange(
			metric.query,
			start,
			end,
			step,
		)

		if err != nil {
			return nil, err
		}
		response.Series = append(
			response.Series,
			MetricSeries{
				Metric: metric.name,
				Points: ParseSeries(result),
			},
		)
	}
	return &response, nil
}
