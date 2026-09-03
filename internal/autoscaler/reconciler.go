package autoscaler

import (
	"context"
	"fmt"
	"log/slog"

	"vulcan/internal/models"
	"vulcan/internal/provisioner"
	"vulcan/internal/repository"
)

type Reconciler struct {
	testRepo       repository.TestRepository
	workerRepo     repository.WorkerRepository
	provisioner    provisioner.Provisioner
	workerCapacity int
	logger         *slog.Logger
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

func (r *Reconciler) Reconcile(ctx context.Context) error {
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

	actual := len(workers)

	r.logger.Info(
		"reconciling worker fleet",
		"desired", desired,
		"actual", actual,
	)

	if actual < desired {
		count := desired - actual

		r.logger.Info(
			"scaling worker fleet up",
			"count", count,
		)

		if err := r.provisioner.Provision(ctx, count); err != nil {
			return fmt.Errorf("provision %d workers: %w", count, err)
		}

		return nil
	}

	if actual <= desired {
		return nil
	}

	excess := actual - desired

	var idle []models.Worker

	for _, worker := range workers {
		if worker.Status == models.WorkerStatusIdle {
			idle = append(idle, worker)
		}
	}

	if len(idle) == 0 {
		r.logger.Info(
			"worker fleet has excess capacity but no idle workers",
			"excess", excess,
		)
		return nil
	}

	if len(idle) > excess {
		idle = idle[:excess]
	}

	workerIDs := make([]string, 0, len(idle))
	for _, worker := range idle {
		workerIDs = append(workerIDs, worker.ID)
	}

	// Atomically transition only workers that are still IDLE to DRAINING.
	// This closes the race between autoscaler discovery and scheduler
	// reservation.
	draining, err := r.workerRepo.TerminateIdleWorkers(ctx, workerIDs)
	if err != nil {
		return fmt.Errorf("mark workers for termination: %w", err)
	}

	if len(draining) == 0 {
		return nil
	}

	hostnameByID := make(map[string]string, len(idle))
	for _, worker := range idle {
		hostnameByID[worker.ID] = worker.Hostname
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
		"scaling worker fleet down",
		"count", len(hostnames),
	)

	if err := r.provisioner.Terminate(ctx, hostnames); err != nil {
		return fmt.Errorf("terminate workers: %w", err)
	}

	return nil
}
