package worker

import (
	"context"
	"log/slog"

	"vulcan/internal/controlplane"
	"vulcan/internal/metrics"
	"vulcan/internal/models"
)

type Worker struct {
	config *Config
	logger *slog.Logger
	client *controlplane.Client
	publisher *metrics.Publisher
	id       string
	hostname string
	status   models.WorkerStatus
	version  string
}

func New(
	config *Config,
	logger *slog.Logger,
	publisher *metrics.Publisher,
) *Worker {

	return &Worker{
		config:    config,
		logger:    logger,
		client:    controlplane.New(config.ControlPlane.URL),
		publisher: publisher,

		hostname: config.Worker.Hostname,
		version:  config.Worker.Version,
		status:   models.WorkerStatusIdle,
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
		w.status = models.WorkerStatusIdle
		return err
	}

	w.logger.Info(
		"assignment marked running",
		"test_id", assignment.TestID,
	)

	if err := w.execute(
		ctx,
		assignment,
	); err != nil {
		w.status = models.WorkerStatusIdle
		return err
	}

	if err := w.client.CompleteAssignment(
		ctx,
		w.id,
		assignment.TestID,
	); err != nil {
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