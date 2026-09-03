package autoscaler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"vulcan/internal/models"
	"vulcan/internal/provisioner"
	"vulcan/internal/repository"
)

// Reconciler is the worker fleet autoscaler. On every reconciliation cycle
// it compares the capacity required by currently active tests against the
// registered worker fleet, provisions any shortfall, and safely drains
// excess idle workers.
//
// The reconciler only manages fleet capacity. It never assigns workers to
// tests -- that remains the scheduler's responsibility.
type Reconciler struct {
	testRepo       repository.TestRepository
	workerRepo     repository.WorkerRepository
	provisioner    provisioner.Provisioner
	workerCapacity int
	logger         *slog.Logger

	// mu ensures at most one reconciliation cycle runs at a time, whether
	// triggered by the periodic Run loop or by an explicit caller (e.g.
	// TestService asking for capacity ahead of a test start). Serializing
	// every entry point through the same lock is what keeps the fleet
	// under a single, coherent provisioning policy instead of racing two
	// independent provisioning decisions against each other.
	mu sync.Mutex
}

func NewReconciler(
	testRepo repository.TestRepository,
	workerRepo repository.WorkerRepository,
	provisioner provisioner.Provisioner,
	workerCapacity int,
	logger *slog.Logger,
) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Reconciler{
		testRepo:       testRepo,
		workerRepo:     workerRepo,
		provisioner:    provisioner,
		workerCapacity: workerCapacity,
		logger:         logger,
	}
}

// Reconcile runs a single reconciliation cycle:
//
//	desired capacity (active tests) vs actual capacity (registered fleet)
//	  desired > actual -> provision the shortfall
//	  desired < actual -> drain excess idle workers
//	  desired == actual -> no action
//
// Only one Reconcile call executes at a time; concurrent callers block on
// the internal lock rather than racing.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tests, err := r.testRepo.GetTests(ctx)
	if err != nil {
		return fmt.Errorf("get tests: %w", err)
	}

	desired, err := DesiredCapacity(tests, r.workerCapacity)
	if err != nil {
		return fmt.Errorf("calculate desired capacity: %w", err)
	}

	workers, err := r.workerRepo.GetWorkers(ctx)
	if err != nil {
		return fmt.Errorf("get workers: %w", err)
	}

	// Only workers that are usable (or about to become usable) capacity
	// count towards "actual". OFFLINE workers are dead. DRAINING workers
	// are already on their way out and must not be double-counted as
	// available fleet capacity nor re-selected for termination.
	actual := 0
	idle := make([]models.Worker, 0, len(workers))

	for _, worker := range workers {
		switch worker.Status {
		case models.WorkerStatusIdle:
			actual++
			idle = append(idle, worker)
		case models.WorkerStatusReserved, models.WorkerStatusRunning:
			actual++
		}
	}

	r.logger.Info(
		"worker fleet reconciliation",
		"desired", desired,
		"actual", actual,
	)

	switch {
	case actual < desired:
		return r.scaleUp(ctx, desired-actual)
	case actual > desired:
		return r.scaleDown(ctx, actual-desired, idle)
	default:
		r.logger.Info("worker fleet already at desired capacity")
		return nil
	}
}

func (r *Reconciler) scaleUp(ctx context.Context, count int) error {
	r.logger.Info("provisioning workers", "count", count)

	if err := r.provisioner.Provision(ctx, count); err != nil {
		return fmt.Errorf("provision %d workers: %w", count, err)
	}

	return nil
}

func (r *Reconciler) scaleDown(ctx context.Context, excess int, idle []models.Worker) error {
	if len(idle) == 0 {
		r.logger.Info(
			"worker fleet has excess capacity but no idle workers",
			"excess", excess,
		)
		return nil
	}

	candidates := idle
	if len(candidates) > excess {
		candidates = candidates[:excess]
	}

	workerIDs := make([]string, 0, len(candidates))
	hostnameByID := make(map[string]string, len(candidates))

	for _, worker := range candidates {
		workerIDs = append(workerIDs, worker.ID)
		hostnameByID[worker.ID] = worker.Hostname
	}

	// Atomically transition only workers that are still IDLE to DRAINING.
	// This closes the race between autoscaler discovery and scheduler
	// reservation: once a worker is DRAINING, the scheduler can no longer
	// reserve it.
	draining, err := r.workerRepo.TerminateIdleWorkers(ctx, workerIDs)
	if err != nil {
		return fmt.Errorf("mark workers for termination: %w", err)
	}

	if len(draining) == 0 {
		return nil
	}

	hostnames := make([]string, 0, len(draining))

	for _, workerID := range draining {
		hostname, ok := hostnameByID[workerID]
		if !ok {
			r.logger.Error(
				"worker marked draining but hostname was not found",
				"worker_id", workerID,
			)
			continue
		}

		hostnames = append(hostnames, hostname)
	}

	if len(hostnames) == 0 {
		return nil
	}

	r.logger.Info("terminating excess workers", "count", len(hostnames))

	// If process termination fails, the worker rows stay DRAINING (we do
	// not revert them to IDLE here). That is intentional: a worker whose
	// process is supposed to be disappearing must never be handed back to
	// the scheduler. The next reconciliation cycle will discover it is
	// still DRAINING and this error surfaces so it can be investigated.
	if err := r.provisioner.Terminate(ctx, hostnames); err != nil {
		return fmt.Errorf("terminate workers: %w", err)
	}

	return nil
}

// Run performs an initial reconciliation and then reconciles on every tick
// of the given interval until ctx is cancelled. It is intentionally
// synchronous: each cycle is driven by a plain ticker loop, not a new
// goroutine per tick, so there can never be more than one reconciliation
// cycle in flight. A failed cycle is logged and the loop continues.
func (r *Reconciler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("reconciliation interval must be greater than zero")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	r.runOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("stopping worker fleet autoscaler")
			return ctx.Err()

		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Reconciler) runOnce(ctx context.Context) {
	if err := r.Reconcile(ctx); err != nil {
		r.logger.Error("worker fleet reconciliation failed", "error", err)
	}
}
