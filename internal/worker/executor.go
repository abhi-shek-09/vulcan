package worker

import (
	"context"
	"time"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/controlplane"
	"vulcan/internal/execution"
	"vulcan/internal/metrics"
)

func (w *Worker) execute(
	ctx context.Context,
	assignment *controlplane.AssignmentResponse,
) error {

	w.logger.Info(
		"starting execution",
		"test_id", assignment.TestID,
	)

	collector := metrics.NewCollector(
		assignment.TestID,
		w.ID(),
	)

	cfg := execution.ExecutionConfig{
		TargetURL:   assignment.TargetURL,
		Method:      assignment.Method,
		Duration:    time.Duration(assignment.DurationSec) * time.Second,
		RPS:         assignment.RPS,
		Concurrency: assignment.Concurrency,
	}

	// execCtx is what actually drives the execution engine. It is
	// cancelled either when the parent context is cancelled (worker
	// shutting down) or when we detect, by polling the control plane,
	// that the test has been stopped externally (see the stopped flag
	// below). Keeping it separate from ctx lets us tell those two cases
	// apart once the engine returns.
	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()

	stopped := false

	executionErr := make(chan error, 1)

	go func() {
		executionErr <- w.executionEngine.Run(
			execCtx,
			cfg,
			func(result execution.Result) {
				collector.Record(metrics.RequestResult{
					Latency:       result.Latency,
					Success:       result.Success,
					BytesSent:     result.BytesSent,
					BytesReceived: result.BytesReceived,
				})
			},
		)
	}()

	flushTicker := time.NewTicker(1 * time.Second)
	defer flushTicker.Stop()

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		case <-flushTicker.C:

			// Ask the control plane whether this test has been stopped.
			// This piggybacks on the existing 1s flush cadence so we
			// don't add extra load, and reacts to STOP roughly as fast
			// as metrics are flushed.
			if !stopped {
				if status, err := w.client.GetTest(ctx, assignment.TestID); err != nil {
					w.logger.Error(
						"failed to check test status",
						"test_id", assignment.TestID,
						"error", err,
					)
				} else if status.Status == "STOPPING" || status.Status == "STOPPED" {
					w.logger.Info(
						"stop detected, cancelling execution",
						"test_id", assignment.TestID,
					)

					stopped = true
					cancelExec()
				}
			}

			bucket := collector.Flush()

			if bucket.Requests == 0 {
				continue
			}

			if err := w.publisher.Publish(ctx, bucket); err != nil {
				w.logger.Error(
					"failed to publish metric bucket",
					"error", err,
				)
			}

		case err := <-executionErr:

			bucket := collector.Flush()

			if bucket.Requests > 0 {
				if pubErr := w.publisher.Publish(ctx, bucket); pubErr != nil {
					w.logger.Error(
						"failed to publish final metric bucket",
						"error", pubErr,
					)
				}
			}

			if stopped {
				w.logger.Info(
					"execution stopped by control plane",
					"test_id", assignment.TestID,
				)

				return apierrors.ErrAssignmentStopped
			}

			if err != nil {
				return err
			}

			w.logger.Info(
				"execution completed",
				"test_id", assignment.TestID,
			)

			return nil
		}
	}
}
