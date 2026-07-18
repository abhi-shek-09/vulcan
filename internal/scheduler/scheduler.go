package scheduler

import (
	"context"
	"fmt"

	"vulcan/internal/models"
	"vulcan/internal/repository"
)

type Scheduler interface {
	AllocateWorkers(
		ctx context.Context,
		testID string,
		workerCount int,
	) ([]models.Worker, error)
}

type DefaultScheduler struct {
	workerRepo repository.WorkerRepository
}

func NewDefaultScheduler(
	workerRepo repository.WorkerRepository,
) *DefaultScheduler {
	return &DefaultScheduler{
		workerRepo: workerRepo,
	}
}

func (s *DefaultScheduler) AllocateWorkers(
	ctx context.Context,
	testID string,
	workerCount int,
) ([]models.Worker, error) {

	workers, err := s.workerRepo.ReserveWorkersForTest(
		ctx,
		testID,
		workerCount,
	)
	if err != nil {
		return nil, fmt.Errorf("allocate workers: %w", err)
	}

	return workers, nil
}