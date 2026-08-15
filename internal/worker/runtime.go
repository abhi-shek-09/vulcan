package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	registerInitialBackoff = 1 * time.Second
	registerMaxBackoff     = 30 * time.Second
)

type Runtime struct {
	logger *slog.Logger
	worker *Worker
	wg     sync.WaitGroup
}

func NewRuntime(logger *slog.Logger, worker *Worker) *Runtime {
	return &Runtime{
		logger: logger,
		worker: worker,
	}
}

func (r *Runtime) Start(ctx context.Context) error {

	r.logger.Info("starting worker runtime")

	// Registration retries with exponential backoff instead of giving up
	// after a single attempt. This matters at startup in particular: the
	// control plane may not be reachable yet (connection refused) or may
	// be slow to respond (request timeout), e.g. when the worker container
	// starts before the server/database are ready. Without a retry loop,
	// a single transient failure here would permanently kill the worker
	// process.
	if err := r.registerWithRetry(ctx); err != nil {
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

// registerWithRetry attempts to register the worker with the control plane,
// retrying with capped exponential backoff on failure (e.g. connection
// refused because the control plane isn't up yet, or a request timeout due
// to a slow/overloaded control plane). It only gives up if the parent
// context is cancelled first (e.g. SIGTERM during startup).
func (r *Runtime) registerWithRetry(ctx context.Context) error {

	backoff := registerInitialBackoff

	for {
		err := r.worker.Register(ctx)
		if err == nil {
			return nil
		}

		r.logger.Warn(
			"worker registration failed, retrying",
			"error", err,
			"retry_in", backoff,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > registerMaxBackoff {
			backoff = registerMaxBackoff
		}
	}
}