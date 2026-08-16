package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"vulcan/internal/controlplane"
	"vulcan/internal/execution"
	"vulcan/internal/metrics"
	"vulcan/internal/models"
)

// ErrAssignmentStopped is returned internally by execute() when it detects,
// via polling the control plane, that the test was stopped externally
// (STOP during execution). It is handled distinctly from real execution
// failures: the control plane has already reconciled the assignment/worker
// state via StopTest, so the worker must not also call FailAssignment or
// CompleteAssignment for it.
var ErrAssignmentStopped = errors.New("assignment stopped by control plane")

type Worker struct {
	config          *Config
	logger          *slog.Logger
	client          *controlplane.Client
	publisher       *metrics.Publisher
	id              string
	hostname        string
	status          models.WorkerStatus
	version         string
	executionEngine execution.Engine
}

func New(
	config *Config,
	logger *slog.Logger,
	publisher *metrics.Publisher,
) *Worker {

	engine := execution.NewEngine(
		execution.NewScheduler(),
		execution.NewHTTPExecutor(30*time.Second),
	)
	return &Worker{
		config:          config,
		logger:          logger,
		client:          controlplane.New(config.ControlPlane.URL),
		publisher:       publisher,
		hostname:        config.Worker.Hostname,
		version:         config.Worker.Version,
		status:          models.WorkerStatusIdle,
		executionEngine: engine,
	}
}

func (w *Worker) Register(ctx context.Context) error {

	req := controlplane.RegisterWorkerRequest{
		Hostname: w.hostname,
		Version:  w.version,
		CPUCount: w.config.Resources.CPUCount,
		MemoryMB: w.config.Resources.MemoryMB,
	}

	resp, err := w.client.RegisterWorker(ctx, req)
	if err != nil {
		return err
	}

	w.id = resp.ID
	w.status = models.WorkerStatus(resp.Status)

	w.logger.Info(
		"worker registered",
		"id", w.id,
		"hostname", w.hostname,
		"status", w.status,
	)

	return nil
}

func (w *Worker) ID() string {
	return w.id
}

func (w *Worker) Status() models.WorkerStatus {
	return w.status
}

func (w *Worker) Hostname() string {
	return w.hostname
}

func (w *Worker) Client() *controlplane.Client {
	return w.client
}

func (w *Worker) sendHeartbeat(ctx context.Context) error {
	return w.client.Heartbeat(
		ctx,
		w.id,
		w.status,
	)
}

func (w *Worker) poll(ctx context.Context) error {

	assignment, err := w.client.GetAssignment(
		ctx,
		w.id,
	)
	if err != nil {
		return err
	}

	if !assignment.Assigned {
		return nil
	}

	return w.processAssignment(
		ctx,
		assignment,
	)
}

func (w *Worker) processAssignment(
	ctx context.Context,
	assignment *controlplane.AssignmentResponse,
) error {

	w.logger.Info(
		"assignment received",
		"test_id", assignment.TestID,
	)

	// Worker is now actively executing a test.
	w.status = models.WorkerStatusRunning

	if err := w.client.StartAssignment(
		ctx,
		w.id,
		assignment.TestID,
	); err != nil {
		w.logger.Error(
			"failed to start assignment",
			"test_id", assignment.TestID,
			"worker_id", w.id,
			"error", err,
		)

		w.status = models.WorkerStatusIdle
		return err
	}

	w.logger.Info(
		"assignment marked running",
		"test_id", assignment.TestID,
	)

	// Execute the assignment and handle failures gracefully.
	if err := w.execute(ctx, assignment); err != nil {

		// The test was stopped from the control plane while we were
		// executing it. The control plane already reconciled the
		// assignment/worker state, so the worker must not attempt
		// another state transition.
		if errors.Is(err, ErrAssignmentStopped) {
			w.logger.Info(
				"assignment stopped externally",
				"test_id", assignment.TestID,
			)

			w.status = models.WorkerStatusIdle
			return nil
		}

		// Do not mark the assignment as failed when the worker itself
		// is shutting down because its parent context was cancelled.
		if ctx.Err() == nil {
			failCtx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()

			if failErr := w.client.FailAssignment(
				failCtx,
				w.id,
				assignment.TestID,
			); failErr != nil {
				w.logger.Error(
					"failed to mark assignment failed",
					"test_id", assignment.TestID,
					"error", failErr,
				)
			}
		}

		w.status = models.WorkerStatusIdle
		return err
	}

	/*
		Execution completed successfully.

		Use an independent short-lived context for the completion
		request. The execution context may already be cancelled or
		expired when the duration ends, and we still need to persist
		the assignment's COMPLETED state.
	*/
	completionCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := w.client.CompleteAssignment(
		completionCtx,
		w.id,
		assignment.TestID,
	); err != nil {
		w.status = models.WorkerStatusIdle
		return err
	}

	// Back to waiting for the next assignment.
	w.status = models.WorkerStatusIdle

	w.logger.Info(
		"assignment completed",
		"test_id", assignment.TestID,
	)

	return nil
}
