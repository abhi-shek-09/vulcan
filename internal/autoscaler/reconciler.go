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

// Reconciler is the worker fleet autoscaler.
//
// It compares the worker fleet against capacity required by:
//   - currently active tests (STARTING/RUNNING)
//   - CREATED tests that are waiting to start
//
// The scheduler remains responsible for assigning workers to tests.
// The reconciler only manages fleet capacity.
type Reconciler struct {
	testRepo       repository.TestRepository
	workerRepo     repository.WorkerRepository
	provisioner    provisioner.Provisioner
	workerCapacity int
	logger         *slog.Logger

	// mu ensures that only one reconciliation cycle runs at a time,
	// regardless of whether reconciliation was triggered by the periodic
	// loop or explicitly by the test service.
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

// Reconcile runs one worker-fleet reconciliation cycle.
//
// Capacity is determined as:
//
//	active test demand  = DesiredCapacity()
//	pending test demand = PendingCapacity()
//
// The fleet target is:
//
//	target = max(active demand, pending demand)
//
// This is important because a CREATED test is not yet active, but it still
// needs workers to exist so that StartTest can reserve them. Without this,
// the autoscaler could drain the entire fleet while a test is waiting in
// CREATED state, leaving StartTest unable to acquire a worker.
//
// Scale-up:
//	target > actual
//	=> provision target-actual workers
//
// Scale-down:
//	target < actual
//	=> drain only excess IDLE workers
//
// RESERVED and RUNNING workers are never terminated by the autoscaler.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tests, err := r.testRepo.GetTests(ctx)
	if err != nil {
		return fmt.Errorf("get tests: %w", err)
	}

	// Active capacity is the demand from STARTING/RUNNING tests.
	desired, err := DesiredCapacity(tests, r.workerCapacity)
	if err != nil {
		return fmt.Errorf("calculate desired capacity: %w", err)
	}

	// Pending capacity is the demand from CREATED tests.
	//
	// Pending tests are included in the fleet target so that workers are
	// not drained before StartTest gets a chance to reserve them.
	pending, err := PendingCapacity(tests, r.workerCapacity)
	if err != nil {
		return fmt.Errorf("calculate pending capacity: %w", err)
	}

	workers, err := r.workerRepo.GetWorkers(ctx)
	if err != nil {
		return fmt.Errorf("get workers: %w", err)
	}

	// Count only workers that are usable fleet capacity.
	//
	// OFFLINE workers are dead and therefore do not count.
	// DRAINING workers are already being removed and therefore do not count.
	//
	// IDLE workers are also collected separately because only IDLE workers
	// are safe candidates for scale-down.
	actual := 0
	idle := make([]models.Worker, 0, len(workers))

	for _, worker := range workers {
		switch worker.Status {
		case models.WorkerStatusIdle:
			actual++
			idle = append(idle, worker)

		case models.WorkerStatusReserved,
			models.WorkerStatusRunning:
			actual++
		}
	}

	// The fleet must be large enough for either active demand or pending
	// demand, whichever is larger.
	//
	// Example:
	//
	//	desired = 0
	//	pending = 1
	//	actual  = 0
	//
	//	target = 1
	//	=> provision 1 worker
	//
	// Another example:
	//
	//	desired = 4
	//	pending = 1
	//	actual  = 2
	//
	//	target = 4
	//	=> provision 2 workers
	target := desired
	if pending > target {
		target = pending
	}

	r.logger.Info(
		"worker fleet reconciliation",
		"desired", desired,
		"pending", pending,
		"target", target,
		"actual", actual,
	)

	switch {
	case actual < target:
		return r.scaleUp(ctx, target-actual)

	case actual > target:
		return r.scaleDown(ctx, actual-target, idle)

	default:
		r.logger.Info(
			"worker fleet already at target capacity",
			"target", target,
		)
		return nil
	}
}

// scaleUp provisions the requested number of additional workers.
func (r *Reconciler) scaleUp(ctx context.Context, count int) error {
	if count <= 0 {
		return nil
	}

	r.logger.Info(
		"provisioning workers",
		"count", count,
	)

	if err := r.provisioner.Provision(ctx, count); err != nil {
		return fmt.Errorf(
			"provision %d workers: %w",
			count,
			err,
		)
	}

	return nil
}

// scaleDown marks excess IDLE workers as DRAINING and then terminates their
// underlying processes.
//
// Only IDLE workers are candidates. RESERVED and RUNNING workers are never
// selected for termination.
func (r *Reconciler) scaleDown(
	ctx context.Context,
	excess int,
	idle []models.Worker,
) error {
	if excess <= 0 {
		return nil
	}

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
	//
	// This closes the race between autoscaler discovery and scheduler
	// reservation. Once a worker is DRAINING, the scheduler can no longer
	// reserve it.
	draining, err := r.workerRepo.TerminateIdleWorkers(
		ctx,
		workerIDs,
	)
	if err != nil {
		return fmt.Errorf(
			"mark workers for termination: %w",
			err,
		)
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

	r.logger.Info(
		"terminating excess workers",
		"count", len(hostnames),
	)

	// If process termination fails, the worker rows remain DRAINING.
	// We intentionally do not return them to IDLE because a worker whose
	// process is supposed to disappear must never be handed back to the
	// scheduler.
	if err := r.provisioner.Terminate(ctx, hostnames); err != nil {
		return fmt.Errorf(
			"terminate workers: %w",
			err,
		)
	}

	return nil
}

// Run performs an initial reconciliation and then reconciles periodically
// until the context is cancelled.
//
// Each reconciliation is synchronous, so there can never be more than one
// reconciliation cycle in flight.
func (r *Reconciler) Run(
	ctx context.Context,
	interval time.Duration,
) error {
	if interval <= 0 {
		return fmt.Errorf(
			"reconciliation interval must be greater than zero",
		)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Perform an initial reconciliation immediately rather than waiting for
	// the first ticker event.
	r.runOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info(
				"stopping worker fleet autoscaler",
			)
			return ctx.Err()

		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

// runOnce executes one reconciliation cycle and logs errors without
// terminating the autoscaler loop.
func (r *Reconciler) runOnce(ctx context.Context) {
	if err := r.Reconcile(ctx); err != nil {
		r.logger.Error(
			"worker fleet reconciliation failed",
			"error", err,
		)
	}
}