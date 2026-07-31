package worker

import (
	"context"
	"log/slog"
	"sync"
)

type Runtime struct {
	logger *slog.Logger
	worker *Worker
	wg sync.WaitGroup
}

func NewRuntime(logger *slog.Logger,worker *Worker) *Runtime {
	return &Runtime{
		logger: logger,
		worker: worker,
	}
}

func (r *Runtime) Start(ctx context.Context) error {

	r.logger.Info("starting worker runtime")
	if err := r.worker.Register(ctx); err != nil {
		return err
	}

	r.logger.Info(
		"worker successfully initialized",
		"id", r.worker.ID(),
		"hostname", r.worker.Hostname(),
	)

	r.wg.Add(1) // increments our wait group counter by 1, signaling that a background task is running.

	go func() { // start a new goroutine
		defer r.wg.Done()
		r.worker.HeartbeatLoop(ctx)
	}()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.worker.PollLoop(ctx)
	}()

	<-ctx.Done()
	// This is a blocking read channel action. Once the background heartbeat is running, the runtime hits this line and stops entirely. It sits here and waits patiently. It will only unblock when the parent application context (ctx) is canceled (e.g., you press CTRL+C or a shutdown signal is dispatched down the pipeline).

	r.logger.Info("shutdown signal received")
	// Wait for all background goroutines to exit.
	r.wg.Wait()
	r.logger.Info("worker runtime stopped")

	return nil
}
