package worker

import (
	"context"
	"time"

	"vulcan/internal/controlplane"
	"vulcan/internal/metrics"
)

func (w *Worker) execute(ctx context.Context, assignment *controlplane.AssignmentResponse) error {

	w.logger.Info(
		"starting execution",
		"test_id", assignment.TestID,
	)
	collector := metrics.NewCollector(
		assignment.TestID,
		w.ID(),
	)
	simulator := metrics.NewSimulator(collector)
	simCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go simulator.Run(simCtx)

	flushTicker := time.NewTicker(1 * time.Second)
	defer flushTicker.Stop()

	executionTimer := time.NewTimer(10 * time.Second)
	defer executionTimer.Stop()

	for {
		select {

		case <-ctx.Done():
			cancel()
			return ctx.Err()

		case <-flushTicker.C:

			bucket := collector.Flush()

			if err := w.publisher.Publish(ctx, bucket); err != nil {
				w.logger.Error(
					"failed to publish metric bucket",
					"error", err,
				)

			} else {
				w.logger.Info(
					"metric bucket published",
					"test_id", bucket.TestID,
					"worker_id", bucket.WorkerID,
					"requests", bucket.Requests,
				)
			}

		case <-executionTimer.C:

			cancel()
			bucket := collector.Flush()

			if bucket.Requests > 0 {
				w.logger.Info(
					"final metric bucket",
					"test_id", bucket.TestID,
					"worker_id", bucket.WorkerID,
					"requests", bucket.Requests,
					"successes", bucket.Successes,
					"failures", bucket.Failures,
					"avg_latency_ms", bucket.AvgLatencyMs,
					"max_latency_ms", bucket.MaxLatencyMs,
					"bytes_sent", bucket.BytesSent,
					"bytes_received", bucket.BytesReceived,
				)
			}

			w.logger.Info(
				"execution completed",
				"test_id", assignment.TestID,
			)

			return nil
		}
	}
}