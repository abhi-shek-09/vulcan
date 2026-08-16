package service

import (
	"context"
	"crypto/rand"
	"github.com/oklog/ulid/v2"
	"strings"
	"time"

	"vulcan/internal/api/apierrors"
	"vulcan/internal/models"
	"vulcan/internal/provisioner"
	"vulcan/internal/repository"
	"vulcan/internal/scheduler"
	"vulcan/internal/validation"
)

type CreateTestRequest struct {
	Name        string `json:"name"`
	WorkerCount int    `json:"worker_count"`
	TargetURL   string `json:"target_url"`
	Method      string `json:"method"`
	DurationSec int    `json:"duration_sec"`
	RPS         int    `json:"rps"`
	Concurrency int    `json:"concurrency"`
}

type TestService struct {
	testRepo          repository.TestRepository
	workerRepo        repository.WorkerRepository
	scheduler         scheduler.Scheduler
	provisioner       provisioner.Provisioner
	workerCapacityRPS int
}

func NewTestService(
	testRepo repository.TestRepository,
	scheduler scheduler.Scheduler,
	workerRepo repository.WorkerRepository,
	workerProvisioner provisioner.Provisioner,
	workerCapacityRPS int,
) *TestService {
	return &TestService{
		testRepo:          testRepo,
		scheduler:         scheduler,
		workerRepo:        workerRepo,
		provisioner:       workerProvisioner,
		workerCapacityRPS: workerCapacityRPS,
	}
}

func (s *TestService) CreateTest(
	ctx context.Context,
	req CreateTestRequest,
) (*models.Test, error) {

	req.Name = strings.TrimSpace(req.Name)

	if err := validation.Required("name", req.Name); err != nil {
		return nil, err
	}

	req.TargetURL = strings.TrimSpace(req.TargetURL)

	if err := validation.Required("target_url", req.TargetURL); err != nil {
		return nil, err
	}

	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))

	switch req.Method {
	case "GET", "POST":
		// valid
	default:
		return nil, apierrors.ErrValidation
	}

	if req.DurationSec <= 0 {
		return nil, apierrors.ErrValidation
	}

	if req.RPS <= 0 {
		return nil, apierrors.ErrValidation
	}

	if req.Concurrency <= 0 {
		return nil, apierrors.ErrValidation
	}

	now := time.Now().UTC()

	test := &models.Test{
		ID:          ulid.MustNew(ulid.Timestamp(now), rand.Reader).String(),
		Name:        req.Name,
		Status:      models.StatusCreated,
		WorkerCount: 0,
		TargetURL:   strings.TrimSpace(req.TargetURL),
		Method:      strings.ToUpper(strings.TrimSpace(req.Method)),
		DurationSec: req.DurationSec,
		RPS:         req.RPS,
		Concurrency: req.Concurrency,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	requiredWorkers, err := scheduler.RequiredWorkers(test.RPS, s.workerCapacityRPS)
	if err != nil {
		return nil, err
	}
	test.WorkerCount = requiredWorkers

	if err := s.testRepo.CreateTest(ctx, test); err != nil {
		return nil, err
	}

	return test, nil
}

func (s *TestService) GetTests(ctx context.Context) ([]models.Test, error) {
	return s.testRepo.GetTests(ctx)
}

func (s *TestService) GetTestByID(ctx context.Context, id string) (*models.Test, error) {
	return s.testRepo.GetTestByID(ctx, id)
}

func (s *TestService) StopTest(ctx context.Context, id string) error {

	test, err := s.testRepo.GetTestByID(ctx, id)
	if err != nil {
		return err
	}

	if test.Status == models.StatusStopped || test.Status == models.StatusCompleted || test.Status == models.StatusFailed {
		return nil
	}

	// Flip to STOPPING first (idempotent no-op if another request already
	// moved it). Workers poll GetTestByID and treat STOPPING/STOPPED as a
	// signal to cancel their in-flight execution instead of running to
	// completion.
	_, err = s.testRepo.TryTransitionStatus(
		ctx,
		id,
		test.Status,
		models.StatusStopping,
	)
	if err != nil {
		return err
	}

	if err := s.workerRepo.ReleaseWorkersForTest(ctx, id); err != nil {
		return err
	}

	if err := s.testRepo.UpdateStatus(
		ctx,
		id,
		models.StatusStopped,
	); err != nil {
		return err
	}

	return nil
}

func (s *TestService) ensureWorkersAvailable(ctx context.Context, required int) error {
	workers, err := s.workerRepo.GetWorkers(ctx)
	if err != nil {
		return err
	}

	idle := 0
	for _, worker := range workers {
		if worker.Status == models.WorkerStatusIdle {
			idle++
		}
	}

	missing := required - idle
	if missing <= 0 {
		return nil
	}
	if s.provisioner == nil {
		return apierrors.ErrInsufficientWorkers
	}

	if err := s.provisioner.Provision(ctx, missing); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		workers, err = s.workerRepo.GetWorkers(waitCtx)
		if err != nil {
			return err
		}

		idle = 0
		for _, worker := range workers {
			if worker.Status == models.WorkerStatusIdle {
				idle++
			}
		}
		if idle >= required {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (s *TestService) StartTest(ctx context.Context, testID string) ([]models.Worker, error) {

	test, err := s.testRepo.GetTestByID(ctx, testID)
	if err != nil {
		return nil, err
	}

	// Atomically claim the right to start this test. This is a single
	// conditional UPDATE (status = CREATED -> STARTING) done directly in
	// Postgres, so if two "start" requests race for the same test, only
	// one of them can win -- the other observes ok == false and is
	// rejected immediately, before any workers are ever allocated. This
	// replaces the old check-then-act pattern (read status, then write
	// status later) which allowed both concurrent requests to pass the
	// check and double-allocate workers for the same test.
	ok, err := s.testRepo.TryTransitionStatus(
		ctx,
		testID,
		models.StatusCreated,
		models.StatusStarting,
	)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, apierrors.ErrInvalidTestState
	}

	requiredWorkers, err := scheduler.RequiredWorkers(test.RPS, s.workerCapacityRPS)
	if err != nil {
		_, _ = s.testRepo.TryTransitionStatus(ctx, testID, models.StatusStarting, models.StatusCreated)
		return nil, err
	}

	if err := s.testRepo.UpdateWorkerCount(ctx, testID, requiredWorkers); err != nil {
		_, _ = s.testRepo.TryTransitionStatus(ctx, testID, models.StatusStarting, models.StatusCreated)
		return nil, err
	}

	if err := s.ensureWorkersAvailable(ctx, requiredWorkers); err != nil {
		_, _ = s.testRepo.TryTransitionStatus(ctx, testID, models.StatusStarting, models.StatusCreated)
		return nil, err
	}

	rpsAllocations, err := scheduler.DistributeRPS(test.RPS, requiredWorkers)
	if err != nil {
		_, _ = s.testRepo.TryTransitionStatus(ctx, testID, models.StatusStarting, models.StatusCreated)
		return nil, err
	}

	workers, err := s.scheduler.AllocateWorkers(
		ctx,
		test.ID,
		requiredWorkers,
		rpsAllocations,
	)
	if err != nil {
		// Roll back so the test can be retried instead of being stuck in
		// STARTING forever (e.g. if there weren't enough idle workers).
		_, _ = s.testRepo.TryTransitionStatus(
			ctx,
			testID,
			models.StatusStarting,
			models.StatusCreated,
		)

		return nil, err
	}

	if err := s.testRepo.UpdateStatus(
		ctx,
		test.ID,
		models.StatusRunning,
	); err != nil {
		return nil, err
	}

	return workers, nil
}
