package reconciler

import (
	"context"
	"log/slog"
	"time"
	"vulcan/internal/repository"
)

type WorkerReconciler struct {
	repo repository.WorkerRepository
	log  *slog.Logger
}

func NewWorkerReconciler(repo repository.WorkerRepository, log *slog.Logger) *WorkerReconciler {
	return &WorkerReconciler{
		repo: repo,
		log:  log,
	}
}

func (r *WorkerReconciler) Start(ctx context.Context) {
	r.log.Info("worker reconciler started")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("stopping background worker health reconciler")
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}

	// select pauses and waits until one of its case channels receives a message.
	// (<-ctx.Done()): Shutdown Pipe. Go app crashes, or you press ctrl+c in your terminal to close the server?, Go closes this context channel.  It logs a shutdown message and runs return, which instantly breaks out of the infinite loop and stops the reconciler cleanly.

	// (<-ticker.C): Timer Pipe. If the app is running normally, this pipe will drop a ball exactly every 5 seconds. When it does, Go executes r.reconcile(ctx), which sweeps your database for dead workers. Once the database work is finished, the loop spins around and the select block pauses again, waiting for the next 5-second alarm.
}

func (r *WorkerReconciler) reconcile(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-15 * time.Second)
	affected, err := r.repo.MarkOfflineWorkers(ctx, cutoff)
	if err != nil {
		r.log.Error("failed to reconcile dead workers", "error", err)
		return
	}

	if affected > 0 {
		r.log.Warn("reconciler marked dead workers as offline", "count", affected)
	}
}
